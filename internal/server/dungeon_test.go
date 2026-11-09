package server

import (
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/store"
	"github.com/kevin/ascii-adventure/internal/world"
)

// nearestDungeon renvoie le donjon du monde de test le plus proche de l'origine.
func nearestDungeon(t *testing.T) *world.Dungeon {
	t.Helper()
	var best *world.Dungeon
	for _, d := range world.New(1).DungeonsNear(0, 0, 4) {
		if best == nil || d.ID < best.ID {
			best = d
		}
	}
	if best == nil {
		t.Fatal("aucun donjon près de l'origine")
	}
	return best
}

func TestWalkingOntoAnEntranceSendsZoneThenDungeonSnapshots(t *testing.T) {
	url, be, srv := startServer(t, 10*time.Millisecond)
	if !srv.Game.Dungeons {
		t.Fatal("les donjons devraient être activés sur le serveur")
	}
	d := nearestDungeon(t)
	register(t, url, "alice")
	waitSavedOnLeave(t, be, "alice")
	be.SaveCharacter("alice", sheet(store.Character{Level: 3, HP: 30, X: d.Entrance.X - 1, Y: d.Entrance.Y}))

	c, w := signInEventually(t, url, "alice")
	if w.X != d.Entrance.X-1 || w.Y != d.Entrance.Y {
		t.Skipf("point de départ déplacé en (%d,%d)", w.X, w.Y)
	}
	readSnapshot(t, c)
	send(t, c, protocol.TMove, protocol.Move{DX: 1})

	var z protocol.Zone
	if err := recvType(t, c, protocol.TZone).Payload(&z); err != nil {
		t.Fatal(err)
	}
	if z.Dungeon != d.ID || z.Name != d.NameIn(i18n.FR) || z.Seed != d.Seed || z.Tier != d.Tier {
		t.Fatalf("zone %+v, attendu le donjon %s", z, d.ID)
	}
	// Le premier instantané après la zone est local au plan, à côté de l'arrivée.
	dm := world.NewDungeonMap(&world.Dungeon{Seed: z.Seed, Tier: z.Tier})
	snap := readSnapshot(t, c)
	if snap.Self.X < 0 || snap.Self.X >= dm.W || snap.Self.Y < 0 || snap.Self.Y >= dm.H ||
		!dm.TileAt(snap.Self.X, snap.Self.Y).Walkable() {
		t.Fatalf("position %+v hors du plan du donjon", snap.Self)
	}
	for _, m := range snap.Monsters {
		if m.X < 0 || m.X >= dm.W || m.Y < 0 || m.Y >= dm.H {
			t.Errorf("monstre de la surface %+v dans un instantané de donjon", m)
		}
	}

	// Déconnecté sous terre, on reprend devant l'entrée.
	c.Close(websocket.StatusNormalClosure, "")
	waitUntil(t, "sortie du donjon", func() bool {
		ch, _ := be.LoadCharacter("alice")
		return ch != nil && ch.X == d.Entrance.X && ch.Y == d.Entrance.Y
	})
	_, w2 := signInEventually(t, url, "alice")
	if abs(w2.X-d.Entrance.X) > 1 || abs(w2.Y-d.Entrance.Y) > 1 {
		t.Errorf("reconnexion en (%d,%d), attendu près de %v", w2.X, w2.Y, d.Entrance)
	}
}

// recvUntil lit des messages jusqu'à en recevoir un du type voulu (au plus 5 s), quel que soit le nombre
// d'instantanés qui s'intercalent.
func recvUntil(t *testing.T, c *websocket.Conn, typ string) protocol.Envelope {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if env := recv(t, c); env.T == typ {
			return env
		}
	}
	t.Fatalf("message %q non reçu", typ)
	return protocol.Envelope{}
}

func TestDiscoveredDungeonsAreSentAtLoginAndSurviveRestart(t *testing.T) {
	url, be, _ := startServer(t, 10*time.Millisecond)
	d := nearestDungeon(t)
	register(t, url, "alice")
	waitSavedOnLeave(t, be, "alice")
	be.SaveCharacter("alice", sheet(store.Character{Level: 3, HP: 30, X: d.Entrance.X - 10, Y: d.Entrance.Y}))

	c, w := signInEventually(t, url, "alice")
	var list protocol.Dungeons
	if err := recvUntil(t, c, protocol.TDungeons).Payload(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.List) != 0 {
		t.Fatalf("liste non vide pour un nouveau personnage: %+v", list)
	}
	// Il suffit de s'approcher à trois cases: le serveur annonce la découverte.
	for i := 0; i < 7; i++ {
		send(t, c, protocol.TMove, protocol.Move{DX: 1})
		time.Sleep(250 * time.Millisecond)
	}
	var found protocol.Dungeons
	if err := recvUntil(t, c, protocol.TDungeons).Payload(&found); err != nil {
		t.Fatal(err)
	}
	if found.New != d.ID || len(found.List) != 1 || found.List[0].X != d.Entrance.X || found.List[0].Tier != d.Tier {
		t.Fatalf("découverte: %+v (départ %v)", found, w)
	}

	// Reconnecté, il retrouve sa liste, sans nouveauté à annoncer.
	c.Close(websocket.StatusNormalClosure, "")
	waitUntil(t, "découverte enregistrée", func() bool {
		ch, _ := be.LoadCharacter("alice")
		return ch != nil && len(ch.Discoveries) == 1
	})
	c2, _ := signInEventually(t, url, "alice")
	var again protocol.Dungeons
	if err := recvUntil(t, c2, protocol.TDungeons).Payload(&again); err != nil {
		t.Fatal(err)
	}
	if len(again.List) != 1 || again.List[0].ID != d.ID || again.New != "" {
		t.Errorf("liste à la reconnexion: %+v", again)
	}
}

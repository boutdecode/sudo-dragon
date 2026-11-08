package server

import (
	"testing"

	"github.com/kevin/ascii-adventure/internal/protocol"
)

func bareSession() *session {
	return &session{send: make(chan []byte, sendBuffer), snapSig: make(chan struct{}, 1)}
}

// Seul le dernier instantané est conservé, sans jamais bloquer l'appelant ni
// toucher à la file des messages fiables.
func TestSetSnapshotKeepsOnlyLatestAndNeverBlocks(t *testing.T) {
	sess := bareSession()
	for i := 1; i <= 10_000; i++ { // bien plus que la capacité de la file fiable
		sess.setSnapshot(protocol.Snapshot{Tick: uint64(i)})
	}

	sess.snapMu.Lock()
	snap := sess.snap
	sess.snapMu.Unlock()
	if snap == nil || snap.Tick != 10_000 {
		t.Fatalf("instantané conservé: %+v, attendu le tick 10000", snap)
	}
	if len(sess.snapSig) != 1 {
		t.Fatalf("%d signaux en attente, attendu exactement 1", len(sess.snapSig))
	}
	if len(sess.send) != 0 {
		t.Fatalf("%d messages fiables: les instantanés ne doivent pas y entrer (sinon un client lent serait déconnecté)", len(sess.send))
	}
}

func TestSetSnapshotCopiesTheValue(t *testing.T) {
	sess := bareSession()
	s := protocol.Snapshot{Tick: 1, Self: protocol.Entity{ID: 7}}
	sess.setSnapshot(s)
	s.Tick, s.Self.ID = 99, 99 // la modification de l'appelant ne doit pas se voir
	if got := sess.snap; got.Tick != 1 || got.Self.ID != 7 {
		t.Fatalf("instantané partagé avec l'appelant: %+v", got)
	}
}

func TestBestEffortDropsWhenQueueIsBusy(t *testing.T) {
	sess := bareSession()
	data := []byte("annonce")

	sess.enqueueBestEffort(data)
	if len(sess.send) != 1 {
		t.Fatalf("file vide: l'annonce devait passer (len=%d)", len(sess.send))
	}

	for len(sess.send) <= sendBuffer/2 { // remplit au-delà de la moitié
		sess.send <- []byte("x")
	}
	before := len(sess.send)
	sess.enqueueBestEffort(data)
	if len(sess.send) != before {
		t.Fatal("une annonce a été ajoutée à une file déjà chargée")
	}
}

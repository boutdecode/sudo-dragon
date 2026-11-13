package server

import (
	"testing"

	"github.com/kevin/ascii-adventure/internal/protocol"
)

func bareSession() *session {
	return &session{send: make(chan []byte, sendBuffer), snapSig: make(chan struct{}, 1)}
}

// Only the latest snapshot is kept, without ever blocking the caller or
// touching the reliable message queue.
func TestSetSnapshotKeepsOnlyLatestAndNeverBlocks(t *testing.T) {
	sess := bareSession()
	for i := 1; i <= 10_000; i++ { // far more than the capacity of the reliable queue
		sess.setSnapshot(protocol.Snapshot{Tick: uint64(i)})
	}

	sess.snapMu.Lock()
	snap := sess.snap
	sess.snapMu.Unlock()
	if snap == nil || snap.Tick != 10_000 {
		t.Fatalf("kept snapshot: %+v, expected tick 10000", snap)
	}
	if len(sess.snapSig) != 1 {
		t.Fatalf("%d pending signals, expected exactly 1", len(sess.snapSig))
	}
	if len(sess.send) != 0 {
		t.Fatalf("%d reliable messages: snapshots must not enter it (otherwise a slow client would be disconnected)", len(sess.send))
	}
}

func TestSetSnapshotCopiesTheValue(t *testing.T) {
	sess := bareSession()
	s := protocol.Snapshot{Tick: 1, Self: protocol.Entity{ID: 7}}
	sess.setSnapshot(s)
	s.Tick, s.Self.ID = 99, 99 // the caller's modification must not be visible
	if got := sess.snap; got.Tick != 1 || got.Self.ID != 7 {
		t.Fatalf("snapshot shared with the caller: %+v", got)
	}
}

func TestBestEffortDropsWhenQueueIsBusy(t *testing.T) {
	sess := bareSession()
	data := []byte("annonce")

	sess.enqueueBestEffort(data)
	if len(sess.send) != 1 {
		t.Fatalf("empty queue: the announcement should have gone through (len=%d)", len(sess.send))
	}

	for len(sess.send) <= sendBuffer/2 { // fills beyond half
		sess.send <- []byte("x")
	}
	before := len(sess.send)
	sess.enqueueBestEffort(data)
	if len(sess.send) != before {
		t.Fatal("an announcement was added to an already loaded queue")
	}
}

package main

// One console socket, ever.
//
// ConnectConsole used to assign a new websocket over the old one and leave it
// connected. The old read loop kept running and its OnOutput closure kept
// emitting, so each extra connect added a permanent second copy of every
// console line — and the window asks to connect from three places. A command
// typed once came back two and three times.
//
// The socket is not reachable from a test without a panel to handshake with,
// so what is checked here is the bookkeeping the fix rests on: the replaced
// socket comes back to the caller to be closed, and a socket that is no longer
// the live one is not treated as though it were.

import (
	"testing"

	"pteroclient-wails/pkg/pterodactyl"
)

func TestOnlyOneConsoleSocketIsEverCurrent(t *testing.T) {
	a := &App{}
	first := &pterodactyl.ConsoleWebSocket{}
	second := &pterodactyl.ConsoleWebSocket{}

	if previous := a.takeConsole(first); previous != nil {
		t.Error("nothing was open, so nothing should have come back to close")
	}
	if !a.consoleIsCurrent(first) {
		t.Fatal("the socket just taken is not the current one")
	}

	// The whole point: what is being replaced has to reach the caller, or it
	// is never closed and never stops emitting.
	previous := a.takeConsole(second)
	if previous != first {
		t.Error("the replaced socket was not handed back; that is the leak")
	}
	if a.consoleIsCurrent(first) {
		t.Error("a replaced socket still counts as live; its output is the duplicate line")
	}
	if !a.consoleIsCurrent(second) {
		t.Error("the new socket is not the current one")
	}

	// An old socket noticing its own close must not clear the live one, and
	// must not report a disconnection the user did not have.
	if a.dropConsole(first) {
		t.Error("an old socket closing reported itself as the live one")
	}
	if !a.consoleIsCurrent(second) {
		t.Error("an old socket closing cleared the live one")
	}

	if !a.dropConsole(second) {
		t.Error("the live socket closing should report itself as the live one")
	}
	if a.currentConsole() != nil {
		t.Error("the socket was not cleared when it closed")
	}
	// Close is reached from both the read loop and Close(); only one of them
	// gets to tell the window.
	if a.dropConsole(second) {
		t.Error("closing twice reported a disconnection twice")
	}
}

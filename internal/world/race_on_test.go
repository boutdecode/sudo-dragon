//go:build race

package world

// raceEnabled is true when the tests are built with -race (which slows execution down a lot).
const raceEnabled = true

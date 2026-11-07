package rubiks

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

func Test4x4SolvedState(t *testing.T) {
	solved := NewSolvedState(4)
	moves, method, err := Solve4x4(string(solved.State))
	if err != nil {
		t.Fatalf("Solve4x4 failed on solved state: %v", err)
	}
	if len(moves) != 0 {
		t.Errorf("Expected 0 moves on solved state, got %d (method %s)", len(moves), method)
	}
}

func Test4x4ShortScramble(t *testing.T) {
	sys := NewSystem(4)
	solved := NewSolvedState(4)

	scrambles := []string{
		"R U R'",
		"2R U 2R'",
		"F R U R' U' F'",
		"2R U2 2R' U2",
	}

	for _, scr := range scrambles {
		c := solved.Clone()
		sys.ApplySeq(c, scr)
		moves, method, err := Solve4x4(string(c.State))
		if err != nil {
			t.Fatalf("Solve4x4 failed on %s: %v", scr, err)
		}
		verifyC := c.Clone()
		sys.ApplySeq(verifyC, strings.Join(moves, " "))
		if !IsUniformlySolved(verifyC) {
			t.Errorf("Scramble %s solved by %s with %d moves did not solve the cube", scr, method, len(moves))
		}
	}
}

func Test4x4ParityFormulas(t *testing.T) {
	sys := NewSystem(4)
	solved := NewSolvedState(4)

	ollScr := "2R2 B2 U2 2L U2 2R' U2 2R U2 F2 2R F2 2L' B2 2R2"
	cOLL := solved.Clone()
	sys.ApplySeq(cOLL, ollScr)
	movesOLL, _, err := Solve4x4(string(cOLL.State))
	if err != nil {
		t.Fatalf("OLL Parity solve failed: %v", err)
	}
	verifyOLL := cOLL.Clone()
	sys.ApplySeq(verifyOLL, strings.Join(movesOLL, " "))
	if !IsUniformlySolved(verifyOLL) {
		t.Errorf("OLL Parity solve did not solve the cube")
	}

	pllScr := "2R2 U2 2R2 Uw2 2R2 2U2"
	cPLL := solved.Clone()
	sys.ApplySeq(cPLL, pllScr)
	movesPLL, _, err := Solve4x4(string(cPLL.State))
	if err != nil {
		t.Fatalf("PLL Parity solve failed: %v", err)
	}
	verifyPLL := cPLL.Clone()
	sys.ApplySeq(verifyPLL, strings.Join(movesPLL, " "))
	if !IsUniformlySolved(verifyPLL) {
		t.Errorf("PLL Parity solve did not solve the cube")
	}
}

func Test4x4SpecificScrambles(t *testing.T) {
	sys := NewSystem(4)
	solved := NewSolvedState(4)
	scrs := []string{
		"U' F' L2 R' D2 B' 2L 2R B 2D2 2D 2R' 2D 2L2 2U2",
		"2F' 2R2 L' U 2R2 2R' 2R 2D2 2D' 2D' U L' F R R' 2F 2D' 2B' 2U F' B' F2 2B2 B 2U' L2 2D2 U2",
		"B2 U2 2L2 U2 2R 2B2 2U 2D 2F' 2F2 B 2B B D' L L' 2U2 D2 F' 2D2 R 2U D R2 2B' 2U2 2L D'",
		"2R2 2L 2F2 2F B2 2R2 D2 2L2 2B 2D' L2 2L 2U' 2R2 L' L' 2R' 2U2 2B2 2U' D' 2R 2F' U' 2F2 U2 D' 2R'",
		"2B R2 2U' 2D' B 2L' D2 2R R' F2 2R2 R2 D 2F' 2L B 2F' 2F2 2U2 2L2 2R L R 2F2 2R 2L2 F 2U2",
	}

	for _, scr := range scrs {
		c := solved.Clone()
		sys.ApplySeq(c, scr)
		moves, method, err := Solve4x4(string(c.State))
		if err != nil {
			t.Fatalf("Solve4x4 failed on %s: %v", scr, err)
		}
		verifyC := c.Clone()
		sys.ApplySeq(verifyC, strings.Join(moves, " "))
		if !IsUniformlySolved(verifyC) {
			t.Errorf("Scramble %s did not solve uniformly (method %s, %d moves)", scr, method, len(moves))
		}
	}
}

func Test4x4Random28Steps(t *testing.T) {
	sys := NewSystem(4)
	solved := NewSolvedState(4)
	rand.Seed(time.Now().UnixNano())

	outerMoves := []string{"U", "U'", "U2", "D", "D'", "D2", "R", "R'", "R2", "L", "L'", "L2", "F", "F'", "F2", "B", "B'", "B2"}
	innerMoves := []string{"2U", "2U'", "2U2", "2D", "2D'", "2D2", "2R", "2R'", "2R2", "2L", "2L'", "2L2", "2F", "2F'", "2F2", "2B", "2B'", "2B2"}
	allPool := append(outerMoves, innerMoves...)

	totalMoves := 0
	const trials = 50
	for trial := 0; trial < trials; trial++ {
		var scr []string
		for i := 0; i < 28; i++ {
			scr = append(scr, allPool[rand.Intn(len(allPool))])
		}
		scrStr := strings.Join(scr, " ")
		c := solved.Clone()
		sys.ApplySeq(c, scrStr)

		moves, method, err := Solve4x4(string(c.State))
		if err != nil {
			t.Fatalf("Trial %d scramble (%s) failed: %v", trial, scrStr, err)
		}
		verifyC := c.Clone()
		sys.ApplySeq(verifyC, strings.Join(moves, " "))
		if !IsUniformlySolved(verifyC) {
			t.Errorf("Trial %d scramble %s did not solve uniformly (method %s, %d moves)", trial, scrStr, method, len(moves))
		}
		totalMoves += len(moves)
	}
	t.Logf("Solved %d/50 28-step scrambles, avg move count: %.1f", trials, float64(totalMoves)/float64(trials))
}

func Test5x5RandomScramble(t *testing.T) {
	sys := NewSystem(5)
	solved := NewSolvedState(5)
	rand.Seed(42)

	pool := []string{
		"U", "U'", "U2", "D", "D'", "D2", "R", "R'", "R2", "L", "L'", "L2", "F", "F'", "F2", "B", "B'", "B2",
		"2U", "2U'", "2U2", "2D", "2D'", "2D2", "2R", "2R'", "2R2", "2L", "2L'", "2L2", "2F", "2F'", "2F2", "2B", "2B'", "2B2",
		"3U", "3U'", "3U2", "3R", "3R'", "3R2", "3F", "3F'", "3F2",
	}

	const trials = 20
	totalMoves := 0
	for trial := 0; trial < trials; trial++ {
		var scr []string
		for i := 0; i < 32; i++ {
			scr = append(scr, pool[rand.Intn(len(pool))])
		}
		scrStr := strings.Join(scr, " ")
		c := solved.Clone()
		sys.ApplySeq(c, scrStr)

		moves, method, err := Solve5x5(string(c.State))
		if err != nil {
			t.Logf("centerMacros5List len = %d", len(centerMacros5List))
			t.Fatalf("5x5 Trial %d scramble (%s) failed: %v", trial, scrStr, err)
		}
		verifyC := c.Clone()
		sys.ApplySeq(verifyC, strings.Join(moves, " "))
		if !IsUniformlySolved(verifyC) {
			t.Fatalf("5x5 Trial %d scramble (%s) did not solve uniformly (method %s, %d moves)", trial, scrStr, method, len(moves))
		}
		totalMoves += len(moves)
	}
	t.Logf("Solved %d/%d 5x5 scrambles, avg move count: %.1f", trials, trials, float64(totalMoves)/float64(trials))
}

func Test6x6RandomScramble(t *testing.T) {
	sys := NewSystem(6)
	solved := NewSolvedState(6)
	rand.Seed(42)

	pool := []string{
		"U", "U'", "U2", "D", "D'", "D2", "R", "R'", "R2", "L", "L'", "L2", "F", "F'", "F2", "B", "B'", "B2",
		"2U", "2U'", "2U2", "2D", "2D'", "2D2", "2R", "2R'", "2R2", "2L", "2L'", "2L2", "2F", "2F'", "2F2", "2B", "2B'", "2B2",
		"3U", "3U'", "3U2", "3D", "3D'", "3D2", "3R", "3R'", "3R2", "3L", "3L'", "3L2", "3F", "3F'", "3F2", "3B", "3B'", "3B2",
	}

	const trials = 20
	totalMoves := 0
	for trial := 0; trial < trials; trial++ {
		var scr []string
		for i := 0; i < 36; i++ {
			scr = append(scr, pool[rand.Intn(len(pool))])
		}
		scrStr := strings.Join(scr, " ")
		c := solved.Clone()
		sys.ApplySeq(c, scrStr)

		moves, method, err := Solve6x6(string(c.State))
		if err != nil {
			t.Fatalf("6x6 Trial %d scramble (%s) failed: %v", trial, scrStr, err)
		}
		verifyC := c.Clone()
		sys.ApplySeq(verifyC, strings.Join(moves, " "))
		if !IsUniformlySolved(verifyC) {
			t.Fatalf("6x6 Trial %d scramble (%s) did not solve uniformly (method %s, %d moves)", trial, scrStr, method, len(moves))
		}
		totalMoves += len(moves)
	}
	t.Logf("Solved %d/%d 6x6 scrambles, avg move count: %.1f", trials, trials, float64(totalMoves)/float64(trials))
}

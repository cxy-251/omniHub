package rubiks

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

func dedgeWingsForDim(dim, k int) [12][2]wingLoc {
	h := dim - 1
	hk := h - k
	return [12][2]wingLoc{
		// 0: UR
		{{0, hk, h, 1, 0, k}, {0, k, h, 1, 0, hk}},
		// 1: UF
		{{0, h, k, 2, 0, k}, {0, h, hk, 2, 0, hk}},
		// 2: UL
		{{0, k, 0, 4, 0, k}, {0, hk, 0, 4, 0, hk}},
		// 3: UB
		{{0, 0, hk, 5, 0, k}, {0, 0, k, 5, 0, hk}},
		// 4: DR
		{{3, hk, h, 1, h, hk}, {3, k, h, 1, h, k}},
		// 5: DF
		{{3, 0, k, 2, h, k}, {3, 0, hk, 2, h, hk}},
		// 6: DL
		{{3, k, 0, 4, h, hk}, {3, hk, 0, 4, h, k}},
		// 7: DB
		{{3, h, hk, 5, h, k}, {3, h, k, 5, h, hk}},
		// 8: FR
		{{2, k, h, 1, k, 0}, {2, hk, h, 1, hk, 0}},
		// 9: FL
		{{2, k, 0, 4, k, h}, {2, hk, 0, 4, hk, h}},
		// 10: BL
		{{5, k, h, 4, k, 0}, {5, hk, h, 4, hk, 0}},
		// 11: BR
		{{5, k, 0, 1, k, h}, {5, hk, 0, 1, hk, h}},
	}
}

func flatWingIdx(dim int, w wingLoc) (int, int) {
	fs := dim * dim
	return w.f1*fs + w.r1*dim + w.c1, w.f2*fs + w.r2*dim + w.c2
}

func buildPermN(sys *System, seq string) []int {
	n := 6 * sys.Dim * sys.Dim
	testC := &CubeState{Dim: sys.Dim, State: make([]byte, n)}
	for i := range testC.State {
		testC.State[i] = byte(i)
	}
	sys.ApplySeq(testC, seq)
	pList := make([]int, n)
	for dst, src := range testC.State {
		pList[dst] = int(src)
	}
	return pList
}

func translateRotatedSeqN(sys *System, r, form string) string {
	if r == "" {
		return form
	}
	n := 6 * sys.Dim * sys.Dim
	invR := sys.InvertSeq(r)
	var out []string
	for _, tok := range strings.Fields(form) {
		conjPerm := buildPermN(sys, r+" "+tok+" "+invR)
		matched := ""
		for name, m := range sys.Moves {
			if m.Axis == 0 {
				continue
			}
			same := true
			for i := 0; i < n; i++ {
				if m.Perm[i] != conjPerm[i] {
					same = false
					break
				}
			}
			if same {
				matched = name
				break
			}
		}
		if matched == "" {
			return r + " " + form + " " + invR
		}
		out = append(out, matched)
	}
	return strings.Join(out, " ")
}

type centerDiffMacro struct {
	seq   string
	diffs [][2]int // pairs of (dst, src) where dst != src
}

var (
	macros5Once       sync.Once
	centerMacros5List []centerDiffMacro
	edgeMacros5List   []macroEntry

	macros6Once       sync.Once
	centerMacros6List []centerDiffMacro
	innerEdge6List    []macroEntry
	outerEdge6List    []macroEntry
)

func all24CubeRotations(sys *System) []string {
	moves := []string{"x", "x'", "x2", "y", "y'", "y2", "z", "z'"}
	solved := NewSolvedState(3)
	sys3 := NewSystem(3)
	type node struct {
		c   *CubeState
		seq string
	}
	q := []node{{c: solved, seq: ""}}
	seen := map[string]bool{string(solved.State): true}
	var out []string
	for len(q) > 0 {
		curr := q[0]
		q = q[1:]
		out = append(out, curr.seq)
		if len(out) == 24 {
			break
		}
		for _, m := range moves {
			nc := curr.c.Clone()
			nc.Apply(sys3.Moves[m].Perm)
			sig := string(nc.State)
			if !seen[sig] {
				seen[sig] = true
				ns := m
				if curr.seq != "" {
					ns = curr.seq + " " + m
				}
				q = append(q, node{c: nc, seq: ns})
			}
		}
	}
	return out
}

func buildCenterDiffMacros(sys *System, baseSwaps []string) []centerDiffMacro {
	rotations := all24CubeRotations(sys)
	solved := NewSolvedState(sys.Dim)
	n := len(solved.State)
	seen := make(map[string]bool)
	var list []centerDiffMacro

	for _, b := range baseSwaps {
		for _, form := range []string{b, sys.InvertSeq(b)} {
			for _, r := range rotations {
				seq := translateRotatedSeqN(sys, r, form)
				if seen[seq] {
					continue
				}
				c := solved.Clone()
				sys.ApplySeq(c, seq)
				diffs := 0
				for i := 0; i < n; i++ {
					if c.State[i] != solved.State[i] {
						diffs++
					}
				}
				if diffs == 2 || diffs == 3 {
					perm := buildPermN(sys, seq)
					var dList [][2]int
					for dst, src := range perm {
						if dst != src {
							dList = append(dList, [2]int{dst, src})
						}
					}
					if len(dList) <= 3 {
						seen[seq] = true
						list = append(list, centerDiffMacro{seq: seq, diffs: dList})
					}
				}
			}
		}
	}
	return list
}

func init5x5Tables(sys *System) {
	macros5Once.Do(func() {
		baseSwaps := []string{
			// X-center commutators (2R / 2L)
			"2R U 2L' U' 2R' U 2L U'",
			"2R U' 2L' U 2R' U' 2L U",
			"2F U 2B' U' 2F' U 2B U'",
			"2F U' 2B' U 2F' U' 2B U",
			"2R F 2L' F' 2R' F 2L F'",
			"2U R 2D' R' 2U' R 2D R'",
			"2U F 2D' F' 2U' F 2D F'",
			"2R U 2L U' 2R' U 2L' U'",
			"2R U' 2L U 2R' U' 2L' U",
			"2F U 2B U' 2F' U 2B' U'",
			"2F U' 2B U 2F' U' 2B' U",
			"2U R 2D R' 2U' R 2D' R'",
			"2U F 2D F' 2U' F 2D' F'",
			// Opposite X-center commutators
			"2R2 U 2L2 U' 2R2 U 2L2 U'",
			"2R2 U' 2L2 U 2R2 U' 2L2 U",
			"2F2 U 2B2 U' 2F2 U 2B2 U'",
			"2F2 U' 2B2 U 2F2 U' 2B2 U",
			"2R2 F 2L2 F' 2R2 F 2L2 F'",
			"2U2 R 2D2 R' 2U2 R 2D2 R'",
			"2U2 F 2D2 F' 2U2 F 2D2 F'",
			// T-center commutators (2R / 3R and 2L / 3R)
			"2R U 3R' U' 2R' U 3R U'",
			"2R U' 3R' U 2R' U' 3R U",
			"2R U 3R U' 2R' U 3R' U'",
			"2R U' 3R U 2R' U' 3R' U",
			"2L' U 3R' U' 2L U 3R U'",
			"2L' U' 3R' U 2L U' 3R U",
			"2L' U 3R U' 2L U 3R' U'",
			"2L' U' 3R U 2L U' 3R' U",
			// Opposite T-center commutators
			"2R2 U 3R2 U' 2R2 U 3R2 U'",
			"2R2 U' 3R2 U 2R2 U' 3R2 U",
			"2L2 U 3R2 U' 2L2 U 3R2 U'",
			"2L2 U' 3R2 U 2L2 U' 3R2 U",
		}
		centerMacros5List = buildCenterDiffMacros(sys, baseSwaps)

		edgeFormulas := []string{
			// 5-move free-slice insertions (preserves FR midge & inner wings, uses U-layer junk slot)
			"2D R U R' 2D'",
			"2D R U' R' 2D'",
			"2D R U2 R' 2D'",
			"2D' R U R' 2D",
			"2D' R U' R' 2D",
			"2D' R U2 R' 2D",
			"2U R U R' 2U'",
			"2U R U' R' 2U'",
			"2U R U2 R' 2U'",
			"2U' R U R' 2U",
			"2U' R U' R' 2U",
			"2U' R U2 R' 2U",
			"2D' L' U' L 2D",
			"2D' L' U L 2D",
			"2D' L' U2 L 2D",
			"2U' L' U' L 2U",
			"2U' L' U L 2U",
			"2U' L' U2 L 2U",
			// Pure 3-cycle of outer wings (preserves all midges, inner wings, and other dedges)
			"R U R' F R' F' R 2D R U R' F R' F' R 2D'",
			"2D R U R' F R' F' R 2D' R U R' F R' F' R",
			"R U R' F R' F' R 2D' R U R' F R' F' R 2D",
			"2D' R U R' F R' F' R 2D R U R' F R' F' R",
			"R U R' F R' F' R 2U R U R' F R' F' R 2U'",
			"2U R U R' F R' F' R 2U' R U R' F R' F' R",
			"R U R' F R' F' R 2U' R U R' F R' F' R 2U",
			"2U' R U R' F R' F' R 2U R U R' F R' F' R",
			// Standard slice-flip-slice & L2E (for double-wing swap / L2E)
			"2D R U R' F R' F' R 2D'",
			"2D' R U R' F R' F' R 2D",
			"2U R U R' F R' F' R 2U'",
			"2U' R U R' F R' F' R 2U",
			"2D R F' U R' F 2D'",
			"2U R F' U R' F 2U'",
			"2D' R F' U R' F 2D",
			"2U' R F' U R' F 2U",
			// Single-dedge wing flip parity (flips the 2 outer wings of UF in place without touching UF midge!)
			"2R2 B2 U2 2L U2 2R' U2 2R U2 F2 2R F2 2L' B2 2R2",
		}
		for _, f := range edgeFormulas {
			edgeMacros5List = append(edgeMacros5List, macroEntry{seq: f, perm: buildPermN(sys, f)})
		}
	})
}

func init6x6Tables(sys *System) {
	macros6Once.Do(func() {
		baseSwaps := []string{
			// Outer X-centers (2R / 2L)
			"2R U 2L' U' 2R' U 2L U'",
			"2R U' 2L' U 2R' U' 2L U",
			"2R U 2L U' 2R' U 2L' U'",
			"2R U' 2L U 2R' U' 2L' U",
			"2R2 U 2L2 U' 2R2 U 2L2 U'",
			"2R2 U' 2L2 U 2R2 U' 2L2 U",
			// Inner X-centers (3R / 3L)
			"3R U 3L' U' 3R' U 3L U'",
			"3R U' 3L' U 3R' U' 3L U",
			"3R U 3L U' 3R' U 3L' U'",
			"3R U' 3L U 3R' U' 3L' U",
			"3R2 U 3L2 U' 3R2 U 3L2 U'",
			"3R2 U' 3L2 U 3R2 U' 3L2 U",
			// Oblique centers (2R / 3L and 2R / 3R and 2L / 3R and 2L / 3L)
			"2R U 3L' U' 2R' U 3L U'",
			"2R U' 3L' U 2R' U' 3L U",
			"2R U 3L U' 2R' U 3L' U'",
			"2R U' 3L U 2R' U' 3L' U",
			"2R2 U 3L2 U' 2R2 U 3L2 U'",
			"2R2 U' 3L2 U 2R2 U' 3L2 U",
			"2R U 3R' U' 2R' U 3R U'",
			"2R U' 3R' U 2R' U' 3R U",
			"2R U 3R U' 2R' U 3R' U'",
			"2R U' 3R U 2R' U' 3R' U",
			"2R2 U 3R2 U' 2R2 U 3R2 U'",
			"2R2 U' 3R2 U 2R2 U' 3R2 U",
			"2L' U 3R' U' 2L U 3R U'",
			"2L' U' 3R' U 2L U' 3R U",
			"2L' U 3R U' 2L U 3R' U'",
			"2L' U' 3R U 2L U' 3R' U",
			"2L2 U 3R2 U' 2L2 U 3R2 U'",
			"2L2 U' 3R2 U 2L2 U' 3R2 U",
			"2L' U 3L' U' 2L U 3L U'",
			"2L' U' 3L' U 2L U' 3L U",
			"2L' U 3L U' 2L U 3L' U'",
			"2L' U' 3L U 2L U' 3L' U",
			"2L2 U 3L2 U' 2L2 U 3L2 U'",
			"2L2 U' 3L2 U 2L2 U' 3L2 U",
		}
		centerMacros6List = buildCenterDiffMacros(sys, baseSwaps)

		// Stage 1 of 6x6 edge pairing: pair the 24 inner wings (3U / 3D)
		innerFormulas := []string{
			"3D R U R' F R' F' R 3D'",
			"3D' R U R' F R' F' R 3D",
			"3U R U R' F R' F' R 3U'",
			"3U' R U R' F R' F' R 3U",
			"3D' L' U' L F' L F L' 3D",
			"3D L' U' L F' L F L' 3D'",
			"3U' L' U' L F' L F L' 3U",
			"3U L' U' L F' L F L' 3U'",
			"3D R F' U R' F 3D'",
			"3U R F' U R' F 3U'",
			"3D' R F' U R' F 3D",
			"3U' R F' U R' F 3U",
			"R U R' F R' F' R 3D R F' U R' F 3D'",
			"R U R' F R' F' R 3U R F' U R' F 3U'",
			"R U R' F R' F' R 3D' R F' U R' F 3D",
			"R U R' F R' F' R 3U' R F' U R' F 3U",
		}
		for _, f := range innerFormulas {
			innerEdge6List = append(innerEdge6List, macroEntry{seq: f, perm: buildPermN(sys, f)})
		}

		// Stage 2 of 6x6 edge pairing: pair the 24 outer wings (2U / 2D) to the inner wings (3U / 3D)
		outerFormulas := []string{
			"2D R U R' 2D'",
			"2D R U' R' 2D'",
			"2D R U2 R' 2D'",
			"2D' R U R' 2D",
			"2D' R U' R' 2D",
			"2D' R U2 R' 2D",
			"2U R U R' 2U'",
			"2U R U' R' 2U'",
			"2U R U2 R' 2U'",
			"2U' R U R' 2U",
			"2U' R U' R' 2U",
			"2U' R U2 R' 2U",
			"2D' L' U' L 2D",
			"2D' L' U L 2D",
			"2D' L' U2 L 2D",
			"2U' L' U' L 2U",
			"2U' L' U L 2U",
			"2U' L' U2 L 2U",
			"R U R' F R' F' R 2D R U R' F R' F' R 2D'",
			"2D R U R' F R' F' R 2D' R U R' F R' F' R",
			"R U R' F R' F' R 2D' R U R' F R' F' R 2D",
			"2D' R U R' F R' F' R 2D R U R' F R' F' R",
			"R U R' F R' F' R 2U R U R' F R' F' R 2U'",
			"2U R U R' F R' F' R 2U' R U R' F R' F' R",
			"R U R' F R' F' R 2U' R U R' F R' F' R 2U",
			"2U' R U R' F R' F' R 2U R U R' F R' F' R",
			"2D R U R' F R' F' R 2D'",
			"2D' R U R' F R' F' R 2D",
			"2U R U R' F R' F' R 2U'",
			"2U' R U R' F R' F' R 2U",
			"2D R F' U R' F 2D'",
			"2U R F' U R' F 2U'",
			"2D' R F' U R' F 2D",
			"2U' R F' U R' F 2U",
			"2R2 B2 U2 2L U2 2R' U2 2R U2 F2 2R F2 2L' B2 2R2",
		}
		for _, f := range outerFormulas {
			outerEdge6List = append(outerEdge6List, macroEntry{seq: f, perm: buildPermN(sys, f)})
		}
	})
}

func calcCenterScoreN(c *CubeState) int {
	dim := c.Dim
	fs := dim * dim
	targetColors := "URFDLB"
	sc := 0
	for f := 0; f < 6; f++ {
		tc := targetColors[f]
		base := f * fs
		for r := 1; r < dim-1; r++ {
			for col := 1; col < dim-1; col++ {
				if c.State[base+r*dim+col] == tc {
					sc++
				}
			}
		}
	}
	return sc
}

func areCentersSolvedN(c *CubeState) bool {
	maxCenters := 6 * (c.Dim - 2) * (c.Dim - 2)
	return calcCenterScoreN(c) == maxCenters
}

func areCentersSolvedAfterN(c *CubeState, perm []int) bool {
	dim := c.Dim
	fs := dim * dim
	targetColors := "URFDLB"
	for f := 0; f < 6; f++ {
		tc := targetColors[f]
		base := f * fs
		for r := 1; r < dim-1; r++ {
			for col := 1; col < dim-1; col++ {
				if c.State[perm[base+r*dim+col]] != tc {
					return false
				}
			}
		}
	}
	return true
}

func centerDeltaAfterDiffMacro(c *CubeState, dm centerDiffMacro) int {
	dim := c.Dim
	fs := dim * dim
	targetColors := "URFDLB"
	delta := 0
	for _, pair := range dm.diffs {
		dst, src := pair[0], pair[1]
		tc := targetColors[dst/fs]
		wasRight := c.State[dst] == tc
		nowRight := c.State[src] == tc
		if nowRight && !wasRight {
			delta++
		} else if !nowRight && wasRight {
			delta--
		}
	}
	return delta
}

func applyDiffMacro(c *CubeState, dm centerDiffMacro) {
	var srcVals [3]byte
	for i, pair := range dm.diffs {
		srcVals[i] = c.State[pair[1]]
	}
	for i, pair := range dm.diffs {
		c.State[pair[0]] = srcVals[i]
	}
}

func solveCentersN(c *CubeState, sys *System, directs []centerDiffMacro) []string {
	outerMoves := []string{"", "U", "U'", "U2", "D", "D'", "D2", "R", "R'", "R2", "L", "L'", "L2", "F", "F'", "F2", "B", "B'", "B2"}
	maxScore := 6 * (c.Dim - 2) * (c.Dim - 2)
	var totalMoves []string

	for step := 0; step < 200; step++ {
		curScore := calcCenterScoreN(c)
		if curScore == maxScore {
			break
		}

		bestDelta := 0
		bestSetup := ""
		bestIdx := -1

		// 1. Single outer setup
		for _, setup := range outerMoves {
			testC := c
			if setup != "" {
				testC = c.Clone()
				testC.Apply(sys.Moves[setup].Perm)
			}
			for i, dm := range directs {
				d := centerDeltaAfterDiffMacro(testC, dm)
				if d > bestDelta {
					bestDelta = d
					bestSetup = setup
					bestIdx = i
				}
			}
		}

		if bestDelta > 0 {
			if bestSetup != "" {
				c.Apply(sys.Moves[bestSetup].Perm)
				totalMoves = append(totalMoves, bestSetup)
			}
			applyDiffMacro(c, directs[bestIdx])
			totalMoves = append(totalMoves, strings.Fields(directs[bestIdx].seq)...)
			continue
		}

		// 2. Two-step outer setup
		found2 := false
		for _, s1 := range outerMoves {
			if s1 == "" {
				continue
			}
			c1 := c.Clone()
			c1.Apply(sys.Moves[s1].Perm)
			for _, s2 := range outerMoves {
				if s2 == "" || s1[0] == s2[0] {
					continue
				}
				c2 := c1.Clone()
				c2.Apply(sys.Moves[s2].Perm)
				for _, dm := range directs {
					if centerDeltaAfterDiffMacro(c2, dm) > 0 {
						c.Apply(sys.Moves[s1].Perm)
						c.Apply(sys.Moves[s2].Perm)
						applyDiffMacro(c, dm)
						totalMoves = append(totalMoves, s1, s2)
						totalMoves = append(totalMoves, strings.Fields(dm.seq)...)
						found2 = true
						break
					}
				}
				if found2 {
					break
				}
			}
			if found2 {
				break
			}
		}
		if found2 {
			continue
		}

		break
	}
	return totalMoves
}

// countOuterMatchedToInner counts how many of the 12 dedge slots have BOTH outer wings (k=1)
// matching the reference inner wings/midge (k=2) of that same slot.
func countOuterSlotsMatched(c *CubeState, outerFlat, innerFlat [12][4]int) int {
	cnt := 0
	for i := 0; i < 12; i++ {
		o := outerFlat[i]
		in := innerFlat[i]
		if c.State[o[0]] == c.State[in[0]] && c.State[o[1]] == c.State[in[1]] &&
			c.State[o[2]] == c.State[in[2]] && c.State[o[3]] == c.State[in[3]] {
			cnt++
		}
	}
	return cnt
}

// countOuterWingsMatched counts individual outer wings (0..24) matched to their slot's inner reference,
// plus 100 * number of fully matched slots (0..12), so BFS prioritizes completing full dedges
// while also being able to make half-dedge progress when needed.
func scoreOuterAgainstInnerAfter(c *CubeState, perm []int, outerFlat, innerFlat [12][4]int) (int, bool) {
	fullSlots := 0
	wings := 0
	for i := 0; i < 12; i++ {
		in := innerFlat[i]
		// Inner wings/midge must remain paired with each other
		if c.State[perm[in[0]]] != c.State[perm[in[2]]] || c.State[perm[in[1]]] != c.State[perm[in[3]]] {
			return -1, false
		}
		o := outerFlat[i]
		w0Match := c.State[perm[o[0]]] == c.State[perm[in[0]]] && c.State[perm[o[1]]] == c.State[perm[in[1]]]
		w1Match := c.State[perm[o[2]]] == c.State[perm[in[2]]] && c.State[perm[o[3]]] == c.State[perm[in[3]]]
		if w0Match {
			wings++
		}
		if w1Match {
			wings++
		}
		if w0Match && w1Match {
			fullSlots++
		}
	}
	return fullSlots*100 + wings, true
}

func currentOuterScore(c *CubeState, outerFlat, innerFlat [12][4]int) int {
	fullSlots := 0
	wings := 0
	for i := 0; i < 12; i++ {
		o := outerFlat[i]
		in := innerFlat[i]
		w0Match := c.State[o[0]] == c.State[in[0]] && c.State[o[1]] == c.State[in[1]]
		w1Match := c.State[o[2]] == c.State[in[2]] && c.State[o[3]] == c.State[in[3]]
		if w0Match {
			wings++
		}
		if w1Match {
			wings++
		}
		if w0Match && w1Match {
			fullSlots++
		}
	}
	return fullSlots*100 + wings
}

func buildFlatSlots(dim, k int) [12][4]int {
	locs := dedgeWingsForDim(dim, k)
	var flat [12][4]int
	for i := 0; i < 12; i++ {
		f1A, f2A := flatWingIdx(dim, locs[i][0])
		f1B, f2B := flatWingIdx(dim, locs[i][1])
		flat[i] = [4]int{f1A, f2A, f1B, f2B}
	}
	return flat
}

func pairOuterToInnerBFS(c *CubeState, sys *System, formulas []macroEntry, outerFlat, innerFlat [12][4]int) []string {
	var totalMoves []string
	outerMoves := []string{"U", "U'", "U2", "D", "D'", "D2", "R", "R'", "R2", "L", "L'", "L2", "F", "F'", "F2", "B", "B'", "B2"}

	type bfsNode struct {
		c    *CubeState
		seq  []string
		last byte
	}

	for step := 0; step < 36; step++ {
		if countOuterSlotsMatched(c, outerFlat, innerFlat) == 12 {
			break
		}
		curScore := currentOuterScore(c, outerFlat, innerFlat)

		queue := []bfsNode{{c: c, seq: nil, last: 0}}
		visited := map[string]bool{string(c.State): true}

		var chosenSetup []string
		chosenFormulaIdx := -1
		bestScore := curScore

		for depth := 0; depth <= 3; depth++ {
			levelSize := len(queue)
			for i := 0; i < levelSize; i++ {
				curr := queue[0]
				queue = queue[1:]

				for fIdx, bm := range formulas {
					if !areCentersSolvedAfterN(curr.c, bm.perm) {
						continue
					}
					sc, ok := scoreOuterAgainstInnerAfter(curr.c, bm.perm, outerFlat, innerFlat)
					if ok && sc > bestScore {
						bestScore = sc
						chosenSetup = curr.seq
						chosenFormulaIdx = fIdx
					}
				}

				if depth < 3 && chosenFormulaIdx == -1 {
					for _, m := range outerMoves {
						if curr.last == m[0] {
							continue
						}
						nextC := curr.c.Clone()
						nextC.Apply(sys.Moves[m].Perm)
						sig := string(nextC.State)
						if !visited[sig] {
							visited[sig] = true
							nextSeq := append(append([]string{}, curr.seq...), m)
							queue = append(queue, bfsNode{c: nextC, seq: nextSeq, last: m[0]})
						}
					}
				}
			}
			if chosenFormulaIdx != -1 {
				break
			}
		}

		if chosenFormulaIdx == -1 {
			break
		}

		for _, m := range chosenSetup {
			c.Apply(sys.Moves[m].Perm)
			totalMoves = append(totalMoves, m)
		}
		c.Apply(formulas[chosenFormulaIdx].perm)
		totalMoves = append(totalMoves, strings.Fields(formulas[chosenFormulaIdx].seq)...)
	}

	return totalMoves
}

func extract3x3CubieN(c *CubeState) (Cubie3, error) {
	var res Cubie3
	dim := c.Dim
	fs := dim * dim
	h := dim - 1

	corners := [8][3][3]int{
		{{0, h, h}, {1, 0, 0}, {2, 0, h}},
		{{0, h, 0}, {2, 0, 0}, {4, 0, h}},
		{{0, 0, 0}, {4, 0, 0}, {5, 0, h}},
		{{0, 0, h}, {5, 0, 0}, {1, 0, h}},
		{{3, 0, h}, {2, h, h}, {1, h, 0}},
		{{3, 0, 0}, {4, h, h}, {2, h, 0}},
		{{3, h, 0}, {5, h, h}, {4, h, 0}},
		{{3, h, h}, {1, h, h}, {5, h, 0}},
	}

	for i := 0; i < 8; i++ {
		c1 := c.State[corners[i][0][0]*fs+corners[i][0][1]*dim+corners[i][0][2]]
		c2 := c.State[corners[i][1][0]*fs+corners[i][1][1]*dim+corners[i][1][2]]
		c3 := c.State[corners[i][2][0]*fs+corners[i][2][1]*dim+corners[i][2][2]]

		foundCorner := -1
		orient := -1
		cols := [3]byte{c1, c2, c3}
		for target := 0; target < 8; target++ {
			tCols := cornerColorNames[target]
			for o := 0; o < 3; o++ {
				if cols[o] == tCols[0] && cols[(o+1)%3] == tCols[1] && cols[(o+2)%3] == tCols[2] {
					foundCorner = target
					orient = o
					break
				}
			}
			if foundCorner != -1 {
				break
			}
		}
		if foundCorner == -1 {
			return res, fmt.Errorf("invalid corner piece at slot %d: %c%c%c", i, c1, c2, c3)
		}
		res.CP[i] = foundCorner
		res.CO[i] = orient
	}

	outerFlat := buildFlatSlots(dim, 1)
	innerFlat := buildFlatSlots(dim, 2)

	for i := 0; i < 12; i++ {
		o := outerFlat[i]
		in := innerFlat[i]
		c1 := c.State[o[0]]
		c2 := c.State[o[1]]
		if c1 != c.State[o[2]] || c2 != c.State[o[3]] || c1 != c.State[in[0]] || c2 != c.State[in[1]] || c1 != c.State[in[2]] || c2 != c.State[in[3]] {
			return res, fmt.Errorf("dedge at slot %d is not fully paired", i)
		}

		foundEdge := -1
		orient := -1
		for target := 0; target < 12; target++ {
			tCols := edgeColorNames[target]
			if c1 == tCols[0] && c2 == tCols[1] {
				foundEdge = target
				orient = 0
				break
			} else if c1 == tCols[1] && c2 == tCols[0] {
				foundEdge = target
				orient = 1
				break
			}
		}
		if foundEdge == -1 {
			return res, fmt.Errorf("invalid edge colors at slot %d: %c%c", i, c1, c2)
		}
		res.EP[i] = foundEdge
		res.EO[i] = orient
	}

	return res, nil
}

func simplifyMovesN(moves []string, sys *System) []string {
	dim := sys.Dim
	cur := moves
	for {
		changed := false
		var next []string
		i := 0
		for i < len(cur) {
			m1, ok := sys.Moves[cur[i]]
			if !ok || m1.Axis == 0 {
				next = append(next, cur[i])
				i++
				continue
			}
			axis := m1.Axis
			turnsByLayer := make([]int, dim)
			j := i
			for j < len(cur) {
				mj, ok := sys.Moves[cur[j]]
				if !ok || mj.Axis != axis {
					break
				}
				turnsByLayer[mj.Layer] = ((turnsByLayer[mj.Layer]+mj.Turns)%4 + 4) % 4
				j++
			}
			var groupOut []string
			for layer := dim - 1; layer >= 0; layer-- {
				t := turnsByLayer[layer]
				if t == 0 {
					continue
				}
				if t == 3 {
					t = -1
				}
				groupOut = append(groupOut, moveNotation(dim, axis, layer, t))
			}
			if len(groupOut) != j-i {
				changed = true
			} else {
				for k := 0; k < len(groupOut); k++ {
					if groupOut[k] != cur[i+k] {
						changed = true
						break
					}
				}
			}
			next = append(next, groupOut...)
			i = j
		}
		cur = next
		if !changed {
			break
		}
	}
	return cur
}

// alignOddCubeCenters finds 0..2 whole-cube rotations (x/y/z) to align the 6 fixed center pieces of a 5x5 cube to URFDLB.
func align5x5FixedCenters(c *CubeState, sys *System) []string {
	checkFixed := func(st *CubeState) bool {
		target := "URFDLB"
		for f := 0; f < 6; f++ {
			if st.State[f*25+12] != target[f] {
				return false
			}
		}
		return true
	}
	if checkFixed(c) {
		return nil
	}
	rots := []string{"3R", "3R'", "3R2", "3U", "3U'", "3U2", "3F", "3F'", "3F2"}
	type node struct {
		st  *CubeState
		seq []string
	}
	q := []node{{st: c.Clone(), seq: nil}}
	for len(q) > 0 {
		curr := q[0]
		q = q[1:]
		if len(curr.seq) >= 4 {
			continue
		}
		for _, r := range rots {
			if len(curr.seq) > 0 && curr.seq[len(curr.seq)-1][:2] == r[:2] {
				continue
			}
			nt := curr.st.Clone()
			nt.Apply(sys.Moves[r].Perm)
			nseq := append(append([]string{}, curr.seq...), r)
			if checkFixed(nt) {
				for _, m := range nseq {
					c.Apply(sys.Moves[m].Perm)
				}
				return nseq
			}
			q = append(q, node{st: nt, seq: nseq})
		}
	}
	return nil
}

// Solve5x5 solves a 5x5x5 cube state (150 chars in URFDLB order).
func Solve5x5(stateStr string) ([]string, string, error) {
	if len(stateStr) != 150 {
		return nil, "", fmt.Errorf("invalid 5x5 state length: expected 150, got %d", len(stateStr))
	}
	sys := NewSystem(5)
	cur := &CubeState{Dim: 5, State: []byte(stateStr)}
	if IsUniformlySolved(cur) {
		return []string{}, "已复原", nil
	}

	init5x5Tables(sys)
	var totalMoves []string

	// 1. Align fixed centers (row=2, col=2) to URFDLB
	rotMoves := align5x5FixedCenters(cur, sys)
	totalMoves = append(totalMoves, rotMoves...)

	// 2. Solve all 48 movable centers (24 T-centers + 24 X-centers)
	centerMoves := solveCentersN(cur, sys, centerMacros5List)
	totalMoves = append(totalMoves, centerMoves...)

	// 3. Pair all 24 outer wings (k=1) to the 12 midges (k=2)
	outerFlat := buildFlatSlots(5, 1)
	midgeFlat := buildFlatSlots(5, 2)
	edgeMoves := pairOuterToInnerBFS(cur, sys, edgeMacros5List, outerFlat, midgeFlat)
	totalMoves = append(totalMoves, edgeMoves...)

	// 4. Extract 3x3 state & solve with Kociemba (5x5 has no OLL/PLL midge parity once wings match midges!)
	cubie, err := extract3x3CubieN(cur)
	if err != nil {
		return nil, "", fmt.Errorf("5阶降阶未能完成全部棱块合并 (中心 %d/54, 已配对 %d/12, macros=%d): %w", calcCenterScoreN(cur), countOuterSlotsMatched(cur, outerFlat, midgeFlat), len(centerMacros5List), err)
	}

	p3x3 := SolveKociemba3(cubie, 30)
	if p3x3 != nil {
		for _, mIdx := range p3x3 {
			mName := MoveNames3[mIdx]
			totalMoves = append(totalMoves, mName)
			cur.Apply(sys.Moves[mName].Perm)
		}
	}

	if !IsUniformlySolved(cur) {
		return nil, "", errors.New("5x5 reduction solution failed to uniformly solve the cube")
	}

	totalMoves = simplifyMovesN(totalMoves, sys)
	return totalMoves, "5阶降阶法（中心归位+中棱对齐+Kociemba）", nil
}

// pairInnerWings6x6 pairs the 24 inner wings (k=2, slices 3U/3D) on a 6x6 cube.
func pairInnerWings6x6(c *CubeState, sys *System, innerFlat [12][4]int) []string {
	var totalMoves []string
	outerMoves := []string{"U", "U'", "U2", "D", "D'", "D2", "R", "R'", "R2", "L", "L'", "L2", "F", "F'", "F2", "B", "B'", "B2"}

	countInner := func(st *CubeState) int {
		cnt := 0
		for i := 0; i < 12; i++ {
			idx := innerFlat[i]
			if st.State[idx[0]] == st.State[idx[2]] && st.State[idx[1]] == st.State[idx[3]] {
				cnt++
			}
		}
		return cnt
	}
	countInnerAfter := func(st *CubeState, perm []int) int {
		cnt := 0
		for i := 0; i < 12; i++ {
			idx := innerFlat[i]
			if st.State[perm[idx[0]]] == st.State[perm[idx[2]]] && st.State[perm[idx[1]]] == st.State[perm[idx[3]]] {
				cnt++
			}
		}
		return cnt
	}

	type bfsNode struct {
		c    *CubeState
		seq  []string
		last byte
	}

	for step := 0; step < 24; step++ {
		curScore := countInner(c)
		if curScore == 12 {
			break
		}

		queue := []bfsNode{{c: c, seq: nil, last: 0}}
		visited := map[string]bool{string(c.State): true}

		var chosenSetup []string
		chosenFormulaIdx := -1
		bestScore := curScore

		for depth := 0; depth <= 3; depth++ {
			levelSize := len(queue)
			for i := 0; i < levelSize; i++ {
				curr := queue[0]
				queue = queue[1:]

				for fIdx, bm := range innerEdge6List {
					if !areCentersSolvedAfterN(curr.c, bm.perm) {
						continue
					}
					sc := countInnerAfter(curr.c, bm.perm)
					if sc > bestScore {
						bestScore = sc
						chosenSetup = curr.seq
						chosenFormulaIdx = fIdx
					}
				}

				if depth < 3 && chosenFormulaIdx == -1 {
					for _, m := range outerMoves {
						if curr.last == m[0] {
							continue
						}
						nextC := curr.c.Clone()
						nextC.Apply(sys.Moves[m].Perm)
						sig := string(nextC.State)
						if !visited[sig] {
							visited[sig] = true
							nextSeq := append(append([]string{}, curr.seq...), m)
							queue = append(queue, bfsNode{c: nextC, seq: nextSeq, last: m[0]})
						}
					}
				}
			}
			if chosenFormulaIdx != -1 {
				break
			}
		}

		if chosenFormulaIdx == -1 {
			break
		}

		for _, m := range chosenSetup {
			c.Apply(sys.Moves[m].Perm)
			totalMoves = append(totalMoves, m)
		}
		c.Apply(innerEdge6List[chosenFormulaIdx].perm)
		totalMoves = append(totalMoves, strings.Fields(innerEdge6List[chosenFormulaIdx].seq)...)
	}

	return totalMoves
}

// Solve6x6 solves a 6x6x6 cube state (216 chars in URFDLB order).
func Solve6x6(stateStr string) ([]string, string, error) {
	if len(stateStr) != 216 {
		return nil, "", fmt.Errorf("invalid 6x6 state length: expected 216, got %d", len(stateStr))
	}
	sys := NewSystem(6)
	cur := &CubeState{Dim: 6, State: []byte(stateStr)}
	if IsUniformlySolved(cur) {
		return []string{}, "已复原", nil
	}

	init6x6Tables(sys)
	var totalMoves []string

	// 1. Solve all 96 centers (24 inner X + 24 outer X + 48 oblique centers)
	centerMoves := solveCentersN(cur, sys, centerMacros6List)
	totalMoves = append(totalMoves, centerMoves...)

	// 2. Pair 24 inner wings (k=2, slices 3U/3D)
	innerFlat := buildFlatSlots(6, 2)
	outerFlat := buildFlatSlots(6, 1)
	innerMoves := pairInnerWings6x6(cur, sys, innerFlat)
	totalMoves = append(totalMoves, innerMoves...)

	// 3. Pair 24 outer wings (k=1, slices 2U/2D) to the 12 inner wing pairs
	outerMoves := pairOuterToInnerBFS(cur, sys, outerEdge6List, outerFlat, innerFlat)
	totalMoves = append(totalMoves, outerMoves...)

	// 4. Extract 3x3 state, fix 6x6 OLL / PLL parities, and solve 3x3 with Kociemba
	cubie, err := extract3x3CubieN(cur)
	if err != nil {
		return nil, "", fmt.Errorf("6阶降阶未能完成全部棱块合并 (中心 %d/96, 已配对 %d/12, macros=%d): %w", calcCenterScoreN(cur), countOuterSlotsMatched(cur, outerFlat, innerFlat), len(centerMacros6List), err)
	}

	// Check OLL Parity (both inner 3-wings and outer 2-wings flip together on UF)
	eoSum := 0
	for _, v := range cubie.EO {
		eoSum += v
	}
	if eoSum%2 != 0 {
		ollSeq := "2R2 3R2 B2 U2 2L 3L U2 2R' 3R' U2 2R 3R U2 F2 2R 3R F2 2L' 3L' B2 2R2 3R2"
		sys.ApplySeq(cur, ollSeq)
		totalMoves = append(totalMoves, strings.Fields(ollSeq)...)
		cubie, err = extract3x3CubieN(cur)
		if err != nil {
			return nil, "", err
		}
	}

	// Check PLL Parity (both inner 3-wings and outer 2-wings swap together)
	permParity := func(p []int) int {
		inv := 0
		for i := 0; i < len(p); i++ {
			for j := i + 1; j < len(p); j++ {
				if p[i] > p[j] {
					inv++
				}
			}
		}
		return inv % 2
	}
	if permParity(cubie.CP[:]) != permParity(cubie.EP[:]) {
		pllSeq := "2R2 3R2 U2 2R2 3R2 U2 2U2 3U2 2R2 3R2 2U2 3U2"
		sys.ApplySeq(cur, pllSeq)
		totalMoves = append(totalMoves, strings.Fields(pllSeq)...)
		cubie, err = extract3x3CubieN(cur)
		if err != nil {
			return nil, "", err
		}
	}

	p3x3 := SolveKociemba3(cubie, 30)
	if p3x3 != nil {
		for _, mIdx := range p3x3 {
			mName := MoveNames3[mIdx]
			totalMoves = append(totalMoves, mName)
			cur.Apply(sys.Moves[mName].Perm)
		}
	}

	if !IsUniformlySolved(cur) {
		return nil, "", errors.New("6x6 reduction solution failed to uniformly solve the cube")
	}

	totalMoves = simplifyMovesN(totalMoves, sys)
	return totalMoves, "6阶降阶法（三轨中心归位+双层翼棱合并+奇偶修正+Kociemba）", nil
}

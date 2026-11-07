package rubiks

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// 4x4 Reduction Solver (Centers -> Edge Pairing -> 3x3 Reduction -> Parity Fixes -> Kociemba)

var centerCoords = [6][4][2]int{
	// U
	{{1, 1}, {1, 2}, {2, 1}, {2, 2}},
	// R
	{{1, 1}, {1, 2}, {2, 1}, {2, 2}},
	// F
	{{1, 1}, {1, 2}, {2, 1}, {2, 2}},
	// D
	{{1, 1}, {1, 2}, {2, 1}, {2, 2}},
	// L
	{{1, 1}, {1, 2}, {2, 1}, {2, 2}},
	// B
	{{1, 1}, {1, 2}, {2, 1}, {2, 2}},
}

var centerIndices = [6][4]int{
	{0*16 + 5, 0*16 + 6, 0*16 + 9, 0*16 + 10},
	{1*16 + 5, 1*16 + 6, 1*16 + 9, 1*16 + 10},
	{2*16 + 5, 2*16 + 6, 2*16 + 9, 2*16 + 10},
	{3*16 + 5, 3*16 + 6, 3*16 + 9, 3*16 + 10},
	{4*16 + 5, 4*16 + 6, 4*16 + 9, 4*16 + 10},
	{5*16 + 5, 5*16 + 6, 5*16 + 9, 5*16 + 10},
}

func areCentersSolved(c *CubeState) bool {
	targetColors := "URFDLB"
	for f := 0; f < 6; f++ {
		tc := targetColors[f]
		for _, idx := range centerIndices[f] {
			if c.State[idx] != tc {
				return false
			}
		}
	}
	return true
}

func areCentersSolvedAfter(c *CubeState, perm []int) bool {
	targetColors := "URFDLB"
	for f := 0; f < 6; f++ {
		tc := targetColors[f]
		for _, idx := range centerIndices[f] {
			if c.State[perm[idx]] != tc {
				return false
			}
		}
	}
	return true
}

func faceletIdx(face, r, c int) int {
	return face*16 + r*4 + c
}

type ShortSolver4 struct {
	sys *System
}

func NewShortSolver4() *ShortSolver4 {
	return &ShortSolver4{sys: NewSystem(4)}
}

func (s *ShortSolver4) SolveShort(state []byte, maxDepth int) []string {
	c := &CubeState{Dim: 4, State: state}
	if IsUniformlySolved(c) {
		return []string{}
	}
	moves := []string{
		"U", "U'", "U2", "D", "D'", "D2", "R", "R'", "R2", "L", "L'", "L2", "F", "F'", "F2", "B", "B'", "B2",
		"2U", "2U'", "2U2", "2D", "2D'", "2D2", "2R", "2R'", "2R2", "2L", "2L'", "2L2", "2F", "2F'", "2F2", "2B", "2B'", "2B2",
	}

	solved := NewSolvedState(4)
	solvedStr := string(solved.State)
	initStr := string(state)

	if initStr == solvedStr {
		return []string{}
	}

	type qNode struct {
		st   string
		path []string
		last string
	}

	forward := map[string][]string{initStr: {}}
	backward := map[string][]string{solvedStr: {}}

	qF := []qNode{{st: initStr, path: nil, last: ""}}
	qB := []qNode{{st: solvedStr, path: nil, last: ""}}

	halfDepth := maxDepth / 2
	if halfDepth < 1 {
		halfDepth = 1
	}
	if halfDepth > 2 {
		halfDepth = 2
	}

	for len(qF) > 0 {
		curr := qF[0]
		qF = qF[1:]
		if len(curr.path) >= halfDepth {
			continue
		}
		currC := &CubeState{Dim: 4, State: []byte(curr.st)}
		for _, m := range moves {
			if curr.last != "" && curr.last[0] == m[0] {
				continue
			}
			nxt := currC.Clone()
			nxt.Apply(s.sys.Moves[m].Perm)
			nxtStr := string(nxt.State)
			if _, ok := forward[nxtStr]; !ok {
				nPath := append(append([]string{}, curr.path...), m)
				forward[nxtStr] = nPath
				if nxtStr == solvedStr {
					return nPath
				}
				qF = append(qF, qNode{st: nxtStr, path: nPath, last: m})
			}
		}
	}

	for len(qB) > 0 {
		curr := qB[0]
		qB = qB[1:]
		if len(curr.path) >= halfDepth {
			continue
		}
		currC := &CubeState{Dim: 4, State: []byte(curr.st)}
		for _, m := range moves {
			if curr.last != "" && curr.last[0] == m[0] {
				continue
			}
			nxt := currC.Clone()
			nxt.Apply(s.sys.Moves[m].Perm)
			nxtStr := string(nxt.State)
			if _, ok := backward[nxtStr]; !ok {
				invM := s.sys.InvertSeq(m)
				nPath := append([]string{invM}, curr.path...)
				backward[nxtStr] = nPath
				if fPath, ok := forward[nxtStr]; ok {
					return append(append([]string{}, fPath...), nPath...)
				}
				qB = append(qB, qNode{st: nxtStr, path: nPath, last: m})
			}
		}
	}

	return nil
}

type wingLoc struct {
	f1, r1, c1 int
	f2, r2, c2 int
}

var dedgeSlots3x3 = [12][2]wingLoc{
	// 0: UR
	{{0, 2, 3, 1, 0, 1}, {0, 1, 3, 1, 0, 2}},
	// 1: UF
	{{0, 3, 1, 2, 0, 1}, {0, 3, 2, 2, 0, 2}},
	// 2: UL
	{{0, 1, 0, 4, 0, 1}, {0, 2, 0, 4, 0, 2}},
	// 3: UB
	{{0, 0, 2, 5, 0, 1}, {0, 0, 1, 5, 0, 2}},
	// 4: DR
	{{3, 2, 3, 1, 3, 2}, {3, 1, 3, 1, 3, 1}},
	// 5: DF
	{{3, 0, 1, 2, 3, 1}, {3, 0, 2, 2, 3, 2}},
	// 6: DL
	{{3, 1, 0, 4, 3, 2}, {3, 2, 0, 4, 3, 1}},
	// 7: DB
	{{3, 3, 2, 5, 3, 1}, {3, 3, 1, 5, 3, 2}},
	// 8: FR
	{{2, 1, 3, 1, 1, 0}, {2, 2, 3, 1, 2, 0}},
	// 9: FL
	{{2, 1, 0, 4, 1, 3}, {2, 2, 0, 4, 2, 3}},
	// 10: BL
	{{5, 1, 3, 4, 1, 0}, {5, 2, 3, 4, 2, 0}},
	// 11: BR
	{{5, 1, 0, 1, 1, 3}, {5, 2, 0, 1, 2, 3}},
}

var dedgeFlatIdx [12][4]int

func init() {
	for i := 0; i < 12; i++ {
		wA := dedgeSlots3x3[i][0]
		wB := dedgeSlots3x3[i][1]
		dedgeFlatIdx[i] = [4]int{
			wA.f1*16 + wA.r1*4 + wA.c1,
			wA.f2*16 + wA.r2*4 + wA.c2,
			wB.f1*16 + wB.r1*4 + wB.c1,
			wB.f2*16 + wB.r2*4 + wB.c2,
		}
	}
}

func getWingColors(c *CubeState, w wingLoc) (byte, byte) {
	return c.State[w.f1*16+w.r1*4+w.c1], c.State[w.f2*16+w.r2*4+w.c2]
}

func isSlotPaired(c *CubeState, slotIdx int) bool {
	idx := dedgeFlatIdx[slotIdx]
	return c.State[idx[0]] == c.State[idx[2]] && c.State[idx[1]] == c.State[idx[3]]
}

func isAllEdgesPaired(c *CubeState) bool {
	for i := 0; i < 12; i++ {
		if !isSlotPaired(c, i) {
			return false
		}
	}
	return true
}

func countPaired(c *CubeState) int {
	cnt := 0
	for i := 0; i < 12; i++ {
		if isSlotPaired(c, i) {
			cnt++
		}
	}
	return cnt
}

func countPairedAfter(c *CubeState, perm []int) int {
	cnt := 0
	for i := 0; i < 12; i++ {
		idx := dedgeFlatIdx[i]
		if c.State[perm[idx[0]]] == c.State[perm[idx[2]]] && c.State[perm[idx[1]]] == c.State[perm[idx[3]]] {
			cnt++
		}
	}
	return cnt
}

type macroEntry struct {
	seq  string
	perm []int
}

var (
	centerMacrosOnce sync.Once
	centerMacrosList []macroEntry
)

func buildPerm(sys *System, seq string) []int {
	testC := &CubeState{Dim: 4, State: make([]byte, 96)}
	for i := range testC.State {
		testC.State[i] = byte(i)
	}
	sys.ApplySeq(testC, seq)
	pList := make([]int, 96)
	for dst, src := range testC.State {
		pList[dst] = int(src)
	}
	return pList
}

// translateRotatedSeq converts r + form + inv(r) into pure single-layer moves (no x/y/z rotations).
func translateRotatedSeq(sys *System, r, form string) string {
	if r == "" {
		return form
	}
	invR := sys.InvertSeq(r)
	var out []string
	for _, tok := range strings.Fields(form) {
		conjPerm := buildPerm(sys, r+" "+tok+" "+invR)
		matched := ""
		for name, m := range sys.Moves {
			if m.Axis == 0 {
				continue // skip wide moves and x/y/z rotations
			}
			same := true
			for i := 0; i < 96; i++ {
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

func getCenterMacros(sys *System) []macroEntry {
	centerMacrosOnce.Do(func() {
		baseSwaps := []string{
			"2R U 2L' U' 2R' U 2L U'",
			"2R U' 2L' U 2R' U' 2L U",
			"2F U 2B' U' 2F' U 2B U'",
			"2F U' 2B' U 2F' U' 2B U",
			"2R F 2L' F' 2R' F 2L F'",
			"2U R 2D' R' 2U' R 2D R'",
			"2U F 2D' F' 2U' F 2D F'",
			// 3-cycles
			"2R U 2L U' 2R' U 2L' U'",
			"2R U' 2L U 2R' U' 2L' U",
			"2F U 2B U' 2F' U 2B' U'",
			"2F U' 2B U 2F' U' 2B' U",
			"2U R 2D R' 2U' R 2D' R'",
			"2U F 2D F' 2U' F 2D' F'",
			// Opposite-face commutators (180-degree slice turns)
			"2R2 U 2L2 U' 2R2 U 2L2 U'",
			"2R2 U' 2L2 U 2R2 U' 2L2 U",
			"2F2 U 2B2 U' 2F2 U 2B2 U'",
			"2F2 U' 2B2 U 2F2 U' 2B2 U",
			"2R2 F 2L2 F' 2R2 F 2L2 F'",
			"2U2 R 2D2 R' 2U2 R 2D2 R'",
			"2U2 F 2D2 F' 2U2 F 2D2 F'",
		}
		rotations := []string{"", "x", "x'", "x2", "y", "y'", "y2", "z", "z'"}
		solved := NewSolvedState(4)
		seen := make(map[string]bool)
		for _, b := range baseSwaps {
			for _, form := range []string{b, sys.InvertSeq(b)} {
				for _, r := range rotations {
					seq := translateRotatedSeq(sys, r, form)
					if seen[seq] {
						continue
					}
					c := solved.Clone()
					sys.ApplySeq(c, seq)
					diffs := 0
					for i := 0; i < 96; i++ {
						if c.State[i] != solved.State[i] {
							diffs++
						}
					}
					if diffs == 2 || diffs == 3 {
						seen[seq] = true
						centerMacrosList = append(centerMacrosList, macroEntry{
							seq:  seq,
							perm: buildPerm(sys, seq),
						})
					}
				}
			}
		}
	})
	return centerMacrosList
}

func calcCenterScore(st *CubeState) int {
	targetColors := "URFDLB"
	sc := 0
	for f := 0; f < 6; f++ {
		tc := targetColors[f]
		for _, idx := range centerIndices[f] {
			if st.State[idx] == tc {
				sc++
			}
		}
	}
	return sc
}

func calcCenterScoreAfter(st *CubeState, perm []int) int {
	targetColors := "URFDLB"
	sc := 0
	for f := 0; f < 6; f++ {
		tc := targetColors[f]
		for _, idx := range centerIndices[f] {
			if st.State[perm[idx]] == tc {
				sc++
			}
		}
	}
	return sc
}

func solveCentersDeterministic(c *CubeState, sys *System) []string {
	directs := getCenterMacros(sys)
	outerMoves := []string{"", "U", "U'", "U2", "D", "D'", "D2", "R", "R'", "R2", "L", "L'", "L2", "F", "F'", "F2", "B", "B'", "B2"}

	var totalMoves []string

	for step := 0; step < 60; step++ {
		curScore := calcCenterScore(c)
		if curScore == 24 {
			break
		}

		bestDelta := 0
		var bestSetup string
		var bestMacroIdx = -1

		// 1. Single outer move setup (no invSetup needed since outer moves preserve calcCenterScore)
		for _, setup := range outerMoves {
			testC := c
			if setup != "" {
				testC = c.Clone()
				testC.Apply(sys.Moves[setup].Perm)
			}
			for i, ds := range directs {
				sc := calcCenterScoreAfter(testC, ds.perm)
				if sc-curScore > bestDelta {
					bestDelta = sc - curScore
					bestSetup = setup
					bestMacroIdx = i
				}
			}
		}

		if bestDelta > 0 {
			if bestSetup != "" {
				c.Apply(sys.Moves[bestSetup].Perm)
				totalMoves = append(totalMoves, bestSetup)
			}
			c.Apply(directs[bestMacroIdx].perm)
			totalMoves = append(totalMoves, strings.Fields(directs[bestMacroIdx].seq)...)
			continue
		}

		// 2. Try all 2-step outer setups (no invSetup needed)
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
				for _, ds := range directs {
					sc := calcCenterScoreAfter(c2, ds.perm)
					if sc > curScore {
						c.Apply(sys.Moves[s1].Perm)
						c.Apply(sys.Moves[s2].Perm)
						c.Apply(ds.perm)
						totalMoves = append(totalMoves, s1, s2)
						totalMoves = append(totalMoves, strings.Fields(ds.seq)...)
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

		// 3. Atomic Buffer Decomposition for opposite 2-swaps
		foundBuf := false
		var wrongPos []int
		targetColors := "URFDLB"
		for f := 0; f < 6; f++ {
			tc := targetColors[f]
			for _, idx := range centerIndices[f] {
				if c.State[idx] != tc {
					wrongPos = append(wrongPos, idx)
				}
			}
		}

		if len(wrongPos) == 2 {
			p1, p2 := wrongPos[0], wrongPos[1]
			var list1, list2 []string
			for _, setup := range outerMoves {
				for _, ds := range directs {
					seq := ds.seq
					if setup != "" {
						seq = setup + " " + ds.seq + " " + sys.InvertSeq(setup)
					}
					testC := c.Clone()
					sys.ApplySeq(testC, seq)
					diff1 := testC.State[p1] != c.State[p1]
					diff2 := testC.State[p2] != c.State[p2]
					if diff1 && !diff2 {
						list1 = append(list1, seq)
					}
					if diff2 && !diff1 {
						list2 = append(list2, seq)
					}
				}
			}

			for _, s1 := range list1 {
				test1 := c.Clone()
				sys.ApplySeq(test1, s1)
				for _, s2 := range list2 {
					test2 := test1.Clone()
					sys.ApplySeq(test2, s2)
					if calcCenterScore(test2) == 24 {
						sys.ApplySeq(c, s1)
						sys.ApplySeq(c, s2)
						totalMoves = append(totalMoves, strings.Fields(s1)...)
						totalMoves = append(totalMoves, strings.Fields(s2)...)
						foundBuf = true
						break
					}
				}
				if foundBuf {
					break
				}
			}
		}

		if foundBuf {
			continue
		}

		break
	}
	return totalMoves
}

var (
	baseEdgeOnce sync.Once
	baseEdgeList []macroEntry
)

func getBaseEdgeFormulas(sys *System) []macroEntry {
	baseEdgeOnce.Do(func() {
		formulas := []string{
			// Standard slice-flip-slice (pairs FR & FL or FR & BR using UR as junk buffer)
			"2D R U R' F R' F' R 2D'",
			"2D' R U R' F R' F' R 2D",
			"2U R U R' F R' F' R 2U'",
			"2U' R U R' F R' F' R 2U",
			// Mirrored FL/BL slice-flip-slice (using UL as junk buffer)
			"2D' L' U' L F' L F L' 2D",
			"2D L' U' L F' L F L' 2D'",
			"2U' L' U' L F' L F L' 2U",
			"2U L' U' L F' L F L' 2U'",
			// L2E (Last Two Edges) pure pairing formulas
			"2D R F' U R' F 2D'",
			"2U R F' U R' F 2U'",
			"2D' R F' U R' F 2D",
			"2U' R F' U R' F 2U",
			"R U R' F R' F' R 2D R F' U R' F 2D'",
			"R U R' F R' F' R 2U R F' U R' F 2U'",
			"R U R' F R' F' R 2D' R F' U R' F 2D",
			"R U R' F R' F' R 2U' R F' U R' F 2U",
		}
		for _, f := range formulas {
			baseEdgeList = append(baseEdgeList, macroEntry{
				seq:  f,
				perm: buildPerm(sys, f),
			})
		}
	})
	return baseEdgeList
}

// pairEdgesDeterministic pairs all 12 dedges using outer-move BFS + base slice-flip-slice formulas.
// Outer moves preserve solved centers and already-paired dedges, so no invSetup is needed.
func pairEdgesDeterministic(c *CubeState, sys *System) []string {
	var totalMoves []string
	baseFormulas := getBaseEdgeFormulas(sys)
	outerMoves := []string{"U", "U'", "U2", "D", "D'", "D2", "R", "R'", "R2", "L", "L'", "L2", "F", "F'", "F2", "B", "B'", "B2"}

	type bfsNode struct {
		c    *CubeState
		seq  []string
		last byte
	}

	for step := 0; step < 24; step++ {
		curScore := countPaired(c)
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

				for fIdx, bm := range baseFormulas {
					if !areCentersSolvedAfter(curr.c, bm.perm) {
						continue
					}
					sc := countPairedAfter(curr.c, bm.perm)
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
		c.Apply(baseFormulas[chosenFormulaIdx].perm)
		totalMoves = append(totalMoves, strings.Fields(baseFormulas[chosenFormulaIdx].seq)...)
	}

	return totalMoves
}

// simplifyMoves cancels redundant/adjacent moves on the same axis and layer.
func simplifyMoves(moves []string, sys *System) []string {
	// Expand wide moves (e.g. Uw2 -> U2 2U2) so individual layers can cancel with adjacent moves
	var expanded []string
	for _, tok := range moves {
		if strings.Contains(tok, "w") {
			base := strings.TrimPrefix(tok, "2")
			face := string(base[0])
			suf := strings.TrimPrefix(base[1:], "w")
			expanded = append(expanded, face+suf, "2"+face+suf)
		} else {
			expanded = append(expanded, tok)
		}
	}

	cur := expanded
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
			var turnsByLayer [4]int
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
			for layer := 3; layer >= 0; layer-- {
				t := turnsByLayer[layer]
				if t == 0 {
					continue
				}
				if t == 3 {
					t = -1
				}
				groupOut = append(groupOut, moveNotation(4, axis, layer, t))
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

var cornerFacelets = [8][3][3]int{
	// 0: URF (U, R, F)
	{{0, 3, 3}, {1, 0, 0}, {2, 0, 3}},
	// 1: UFL (U, F, L)
	{{0, 3, 0}, {2, 0, 0}, {4, 0, 3}},
	// 2: ULB (U, L, B)
	{{0, 0, 0}, {4, 0, 0}, {5, 0, 3}},
	// 3: UBR (U, B, R)
	{{0, 0, 3}, {5, 0, 0}, {1, 0, 3}},
	// 4: DFR (D, F, R)
	{{3, 0, 3}, {2, 3, 3}, {1, 3, 0}},
	// 5: DLF (D, L, F)
	{{3, 0, 0}, {4, 3, 3}, {2, 3, 0}},
	// 6: DBL (D, B, L)
	{{3, 3, 0}, {5, 3, 3}, {4, 3, 0}},
	// 7: DRB (D, R, B)
	{{3, 3, 3}, {1, 3, 3}, {5, 3, 0}},
}

var cornerColorNames = [8][3]byte{
	{'U', 'R', 'F'},
	{'U', 'F', 'L'},
	{'U', 'L', 'B'},
	{'U', 'B', 'R'},
	{'D', 'F', 'R'},
	{'D', 'L', 'F'},
	{'D', 'B', 'L'},
	{'D', 'R', 'B'},
}

var edgeColorNames = [12][2]byte{
	{'U', 'R'},
	{'U', 'F'},
	{'U', 'L'},
	{'U', 'B'},
	{'D', 'R'},
	{'D', 'F'},
	{'D', 'L'},
	{'D', 'B'},
	{'F', 'R'},
	{'F', 'L'},
	{'B', 'L'},
	{'B', 'R'},
}

func extract3x3Cubie(c *CubeState) (Cubie3, error) {
	var res Cubie3

	for i := 0; i < 8; i++ {
		c1 := c.State[cornerFacelets[i][0][0]*16+cornerFacelets[i][0][1]*4+cornerFacelets[i][0][2]]
		c2 := c.State[cornerFacelets[i][1][0]*16+cornerFacelets[i][1][1]*4+cornerFacelets[i][1][2]]
		c3 := c.State[cornerFacelets[i][2][0]*16+cornerFacelets[i][2][1]*4+cornerFacelets[i][2][2]]

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

	for i := 0; i < 12; i++ {
		wA := dedgeSlots3x3[i][0]
		wB := dedgeSlots3x3[i][1]
		c1A := c.State[wA.f1*16+wA.r1*4+wA.c1]
		c2A := c.State[wA.f2*16+wA.r2*4+wA.c2]
		c1B := c.State[wB.f1*16+wB.r1*4+wB.c1]
		c2B := c.State[wB.f2*16+wB.r2*4+wB.c2]

		if c1A != c1B || c2A != c2B {
			return res, fmt.Errorf("dedge at slot %d is not paired: (%c,%c) vs (%c,%c)", i, c1A, c2A, c1B, c2B)
		}

		foundEdge := -1
		orient := -1
		for target := 0; target < 12; target++ {
			tCols := edgeColorNames[target]
			if c1A == tCols[0] && c2A == tCols[1] {
				foundEdge = target
				orient = 0
				break
			} else if c1A == tCols[1] && c2A == tCols[0] {
				foundEdge = target
				orient = 1
				break
			}
		}
		if foundEdge == -1 {
			return res, fmt.Errorf("invalid edge colors at slot %d: %c%c", i, c1A, c2A)
		}
		res.EP[i] = foundEdge
		res.EO[i] = orient
	}

	return res, nil
}

// Solve4x4 is the main entry point for 4x4 solving
func Solve4x4(stateStr string) ([]string, string, error) {
	if len(stateStr) != 96 {
		return nil, "", fmt.Errorf("invalid 4x4 state length: expected 96, got %d", len(stateStr))
	}
	sys := NewSystem(4)
	cur := &CubeState{Dim: 4, State: []byte(stateStr)}

	if IsUniformlySolved(cur) {
		return []string{}, "已复原", nil
	}

	// 1. Try short exact bidirectional BFS up to depth 4
	shortSol := NewShortSolver4().SolveShort(cur.State, 4)
	if shortSol != nil {
		return shortSol, "双向 BFS 最短解", nil
	}

	var totalMoves []string

	// 2. Check if already legal 3x3 state (e.g. pure outer scramble)
	if areCentersSolved(cur) && isAllEdgesPaired(cur) {
		if _, err := extract3x3Cubie(cur); err == nil {
			goto Solve3x3Stage
		}
	}

	// 3. Solve 6 Centers
	{
		centerMoves := solveCentersDeterministic(cur, sys)
		totalMoves = append(totalMoves, centerMoves...)
	}

	// 4. Pair 12 dedges
	{
		edgeMoves := pairEdgesDeterministic(cur, sys)
		totalMoves = append(totalMoves, edgeMoves...)
	}

Solve3x3Stage:
	// 5. Check Parity & Solve 3x3 Phase
	cubie, err := extract3x3Cubie(cur)
	if err != nil {
		return nil, "", fmt.Errorf("降阶未能完成全部棱块合并 (已配对 %d/12): %w", countPaired(cur), err)
	}

	// Check OLL Parity
	eoSum := 0
	for _, v := range cubie.EO {
		eoSum += v
	}
	if eoSum%2 != 0 {
		ollSeq := "2R2 B2 U2 2L U2 2R' U2 2R U2 F2 2R F2 2L' B2 2R2"
		sys.ApplySeq(cur, ollSeq)
		totalMoves = append(totalMoves, strings.Fields(ollSeq)...)
		cubie, err = extract3x3Cubie(cur)
		if err != nil {
			return nil, "", err
		}
	}

	// Check PLL Parity
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
		pllSeq := "2R2 U2 2R2 U2 2U2 2R2 2U2"
		sys.ApplySeq(cur, pllSeq)
		totalMoves = append(totalMoves, strings.Fields(pllSeq)...)
		cubie, err = extract3x3Cubie(cur)
		if err != nil {
			return nil, "", err
		}
	}

	// Solve 3x3 with Kociemba
	p3x3 := SolveKociemba3(cubie, 30)
	if p3x3 != nil {
		for _, mIdx := range p3x3 {
			mName := MoveNames3[mIdx]
			totalMoves = append(totalMoves, mName)
			m := sys.Moves[mName]
			cur.Apply(m.Perm)
		}
	}

	if !IsUniformlySolved(cur) {
		return nil, "", errors.New("4x4 reduction solution failed to uniformly solve the cube")
	}

	totalMoves = simplifyMoves(totalMoves, sys)
	return totalMoves, "4阶降阶法（中心归位+BFS配棱+奇偶修正+Kociemba）", nil
}

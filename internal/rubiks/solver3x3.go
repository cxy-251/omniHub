package rubiks

type Cubie3 struct {
	CP [8]int
	CO [8]int
	EP [12]int
	EO [12]int
}

var Solved3 = Cubie3{
	CP: [8]int{0, 1, 2, 3, 4, 5, 6, 7},
	CO: [8]int{0, 0, 0, 0, 0, 0, 0, 0},
	EP: [12]int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
	EO: [12]int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
}

func MulCubie3(a, b Cubie3) Cubie3 {
	var res Cubie3
	for i := 0; i < 8; i++ {
		res.CP[i] = a.CP[b.CP[i]]
		res.CO[i] = (a.CO[b.CP[i]] + b.CO[i]) % 3
	}
	for i := 0; i < 12; i++ {
		res.EP[i] = a.EP[b.EP[i]]
		res.EO[i] = (a.EO[b.EP[i]] + b.EO[i]) % 2
	}
	return res
}

var basicMoves3 = []Cubie3{
	{CP: [8]int{3, 0, 1, 2, 4, 5, 6, 7}, CO: [8]int{0, 0, 0, 0, 0, 0, 0, 0}, EP: [12]int{3, 0, 1, 2, 4, 5, 6, 7, 8, 9, 10, 11}, EO: [12]int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
	{CP: [8]int{4, 1, 2, 0, 7, 5, 6, 3}, CO: [8]int{2, 0, 0, 1, 1, 0, 0, 2}, EP: [12]int{8, 1, 2, 3, 11, 5, 6, 7, 4, 9, 10, 0}, EO: [12]int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
	{CP: [8]int{1, 5, 2, 3, 0, 4, 6, 7}, CO: [8]int{1, 2, 0, 0, 2, 1, 0, 0}, EP: [12]int{0, 9, 2, 3, 4, 8, 6, 7, 1, 5, 10, 11}, EO: [12]int{0, 1, 0, 0, 0, 1, 0, 0, 1, 1, 0, 0}},
	{CP: [8]int{0, 1, 2, 3, 5, 6, 7, 4}, CO: [8]int{0, 0, 0, 0, 0, 0, 0, 0}, EP: [12]int{0, 1, 2, 3, 5, 6, 7, 4, 8, 9, 10, 11}, EO: [12]int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
	{CP: [8]int{0, 2, 6, 3, 4, 1, 5, 7}, CO: [8]int{0, 1, 2, 0, 0, 2, 1, 0}, EP: [12]int{0, 1, 10, 3, 4, 5, 9, 7, 8, 2, 6, 11}, EO: [12]int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
	{CP: [8]int{0, 1, 3, 7, 4, 5, 2, 6}, CO: [8]int{0, 0, 1, 2, 0, 0, 2, 1}, EP: [12]int{0, 1, 2, 11, 4, 5, 6, 10, 8, 9, 3, 7}, EO: [12]int{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 1, 1}},
}

var moves18_3 [18]Cubie3

func initMoves3() {
	for i, b := range basicMoves3 {
		m2 := MulCubie3(b, b)
		m3 := MulCubie3(m2, b)
		moves18_3[i*3] = b
		moves18_3[i*3+1] = m2
		moves18_3[i*3+2] = m3
	}
}

var MoveNames3 = []string{
	"U", "U2", "U'", "R", "R2", "R'", "F", "F2", "F'",
	"D", "D2", "D'", "L", "L2", "L'", "B", "B2", "B'",
}

func lehmer(a []int) int {
	r := 0
	for i := 0; i < len(a); i++ {
		c := 0
		for j := i + 1; j < len(a); j++ {
			if a[j] < a[i] {
				c++
			}
		}
		r = r*(len(a)-i) + c
	}
	return r
}

var sliceIndexMap [4096]int

func initSliceIndex() {
	k := 0
	for m := 0; m < 4096; m++ {
		b := 0
		for t := m; t != 0; t &= t - 1 {
			b++
		}
		if b == 4 {
			sliceIndexMap[m] = k
			k++
		}
	}
}

func twistOf(c Cubie3) int {
	t := 0
	for i := 0; i < 7; i++ {
		t = t*3 + c.CO[i]
	}
	return t
}

func flipOf(c Cubie3) int {
	f := 0
	for i := 0; i < 11; i++ {
		f = f*2 + c.EO[i]
	}
	return f
}

func sliceOf(c Cubie3) int {
	m := 0
	for i := 0; i < 12; i++ {
		if c.EP[i] >= 8 {
			m |= 1 << i
		}
	}
	return sliceIndexMap[m]
}

func cpOf(c Cubie3) int {
	return lehmer(c.CP[:])
}

func ep8Of(c Cubie3) int {
	return lehmer(c.EP[:8])
}

func spOf(c Cubie3) int {
	s := []int{c.EP[8] - 8, c.EP[9] - 8, c.EP[10] - 8, c.EP[11] - 8}
	return lehmer(s)
}

type Solver3Tables struct {
	twist  []int32
	flip   []int32
	slice  []int32
	cp     []int32
	ep8    []int32
	sp     []int32
	pTwist []int8
	pFlip  []int8
	pCp    []int8
	pEp    []int8
	slice0 int
}

var globalTables3 *Solver3Tables

var all18Moves = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17}
var p2Moves = []int{0, 1, 2, 9, 10, 11, 4, 13, 7, 16}

func buildMoveTable(size int, coord func(Cubie3) int, moves []int) []int32 {
	table := make([]int32, size*18)
	for i := range table {
		table[i] = -1
	}
	rep := make([]*Cubie3, size)
	start := coord(Solved3)
	rep[start] = &Solved3
	queue := []int{start}
	for h := 0; h < len(queue); h++ {
		c := queue[h]
		cube := *rep[c]
		for _, m := range moves {
			next := MulCubie3(cube, moves18_3[m])
			nc := coord(next)
			table[c*18+m] = int32(nc)
			if rep[nc] == nil {
				rep[nc] = &next
				queue = append(queue, nc)
			}
		}
	}
	return table
}

func buildPruneTable(n1, n2 int, t1, t2 []int32, s1, s2 int, moves []int) []int8 {
	dist := make([]int8, n1*n2)
	for i := range dist {
		dist[i] = -1
	}
	queue := make([]int32, n1*n2)
	head, tail := 0, 0
	start := s1*n2 + s2
	dist[start] = 0
	queue[tail] = int32(start)
	tail++

	for head < tail {
		idx := int(queue[head])
		head++
		a := idx / n2
		b := idx % n2
		d := dist[idx]
		for _, m := range moves {
			na := int(t1[a*18+m])
			nb := int(t2[b*18+m])
			ni := na*n2 + nb
			if dist[ni] < 0 {
				dist[ni] = d + 1
				queue[tail] = int32(ni)
				tail++
			}
		}
	}
	return dist
}

func GetSolver3Tables() *Solver3Tables {
	if globalTables3 != nil {
		return globalTables3
	}
	initMoves3()
	initSliceIndex()

	twist := buildMoveTable(2187, twistOf, all18Moves)
	flip := buildMoveTable(2048, flipOf, all18Moves)
	slice := buildMoveTable(495, sliceOf, all18Moves)
	cp := buildMoveTable(40320, cpOf, p2Moves)
	ep8 := buildMoveTable(40320, ep8Of, p2Moves)
	sp := buildMoveTable(24, spOf, p2Moves)
	s0 := sliceOf(Solved3)

	globalTables3 = &Solver3Tables{
		twist:  twist,
		flip:   flip,
		slice:  slice,
		cp:     cp,
		ep8:    ep8,
		sp:     sp,
		slice0: s0,
		pTwist: buildPruneTable(495, 2187, slice, twist, s0, 0, all18Moves),
		pFlip:  buildPruneTable(495, 2048, slice, flip, s0, 0, all18Moves),
		pCp:    buildPruneTable(40320, 24, cp, sp, 0, 0, p2Moves),
		pEp:    buildPruneTable(40320, 24, ep8, sp, 0, 0, p2Moves),
	}
	return globalTables3
}

func allowedMove(last, m int) bool {
	if last < 0 {
		return true
	}
	f := m / 3
	lf := last / 3
	return f != lf && (lf-f != 3)
}

func SolveKociemba3(c Cubie3, maxTotal int) []int {
	T := GetSolver3Tables()
	var path []int
	var path2 []int

	var search2 func(cp, ep, sp, depth, last int) bool
	search2 = func(cp, ep, sp, depth, last int) bool {
		if depth == 0 {
			return cp == 0 && ep == 0 && sp == 0
		}
		cPrune := int(T.pCp[cp*24+sp])
		ePrune := int(T.pEp[ep*24+sp])
		maxPrune := cPrune
		if ePrune > maxPrune {
			maxPrune = ePrune
		}
		if maxPrune > depth {
			return false
		}
		for _, m := range p2Moves {
			if !allowedMove(last, m) {
				continue
			}
			path2 = append(path2, m)
			if search2(int(T.cp[cp*18+m]), int(T.ep8[ep*18+m]), int(T.sp[sp*18+m]), depth-1, m) {
				return true
			}
			path2 = path2[:len(path2)-1]
		}
		return false
	}

	phase2 := func() bool {
		cube := c
		for _, m := range path {
			cube = MulCubie3(cube, moves18_3[m])
		}
		cp := cpOf(cube)
		ep := ep8Of(cube)
		sp := spOf(cube)
		for d := 0; d <= maxTotal-len(path); d++ {
			path2 = path2[:0]
			if search2(cp, ep, sp, d, -1) {
				return true
			}
		}
		return false
	}

	var search1 func(tw, fl, sl, depth, last int) bool
	search1 = func(tw, fl, sl, depth, last int) bool {
		if depth == 0 {
			return tw == 0 && fl == 0 && sl == T.slice0 && phase2()
		}
		tPrune := int(T.pTwist[sl*2187+tw])
		fPrune := int(T.pFlip[sl*2048+fl])
		maxPrune := tPrune
		if fPrune > maxPrune {
			maxPrune = fPrune
		}
		if maxPrune > depth {
			return false
		}
		for _, m := range all18Moves {
			if !allowedMove(last, m) {
				continue
			}
			path = append(path, m)
			if search1(int(T.twist[tw*18+m]), int(T.flip[fl*18+m]), int(T.slice[sl*18+m]), depth-1, m) {
				return true
			}
			path = path[:len(path)-1]
		}
		return false
	}

	tw := twistOf(c)
	fl := flipOf(c)
	sl := sliceOf(c)

	for d := 0; d <= 14; d++ {
		path = path[:0]
		if search1(tw, fl, sl, d, -1) {
			raw := append(path, path2...)
			return mergeMoves3(raw)
		}
	}
	return nil
}

func mergeMoves3(moves []int) []int {
	var out []int
	for _, m := range moves {
		if len(out) > 0 && out[len(out)-1]/3 == m/3 {
			last := out[len(out)-1]
			out = out[:len(out)-1]
			turns := ((last%3 + 1) + (m%3 + 1)) % 4
			if turns != 0 {
				out = append(out, (m/3)*3+turns-1)
			}
		} else {
			out = append(out, m)
		}
	}
	return out
}

package rubiks

import (
	"fmt"
	"strings"
)

type Move struct {
	Axis     rune
	Layer    int
	Turns    int // 1, -1, 2
	Notation string
	Perm     []int
}

type System struct {
	Dim   int
	Geom  [][6]int
	Index map[[6]int]int
	Moves map[string]Move
}

func rotateVector(v [3]int, axis rune, turns int) [3]int {
	t := (turns%4 + 4) % 4
	x, y, z := v[0], v[1], v[2]
	for i := 0; i < t; i++ {
		switch axis {
		case 'x':
			y, z = -z, y
		case 'y':
			x, z = z, -x
		case 'z':
			x, y = -y, x
		}
	}
	return [3]int{x, y, z}
}

func FaceletGeometry(dim int) [][6]int {
	high := dim - 1
	vals := make([]int, dim)
	for i := 0; i < dim; i++ {
		vals[i] = 2*i - high
	}
	var geom [][6]int
	for _, face := range "URFDLB" {
		for row := 0; row < dim; row++ {
			for col := 0; col < dim; col++ {
				var pos, norm [3]int
				switch face {
				case 'U':
					pos = [3]int{vals[col], high, vals[row]}
					norm = [3]int{0, 1, 0}
				case 'R':
					pos = [3]int{high, -vals[row], -vals[col]}
					norm = [3]int{1, 0, 0}
				case 'F':
					pos = [3]int{vals[col], -vals[row], high}
					norm = [3]int{0, 0, 1}
				case 'D':
					pos = [3]int{vals[col], -high, -vals[row]}
					norm = [3]int{0, -1, 0}
				case 'L':
					pos = [3]int{-high, -vals[row], vals[col]}
					norm = [3]int{-1, 0, 0}
				case 'B':
					pos = [3]int{-vals[col], -vals[row], -high}
					norm = [3]int{0, 0, -1}
				}
				geom = append(geom, [6]int{pos[0], pos[1], pos[2], norm[0], norm[1], norm[2]})
			}
		}
	}
	return geom
}

func moveNotation(dim int, axis rune, layer, turns int) string {
	posFace := map[rune]string{'x': "R", 'y': "U", 'z': "F"}[axis]
	negFace := map[rune]string{'x': "L", 'y': "D", 'z': "B"}[axis]
	usePos := layer*2 >= dim-1
	face := negFace
	depth := layer + 1
	baseTurn := 1
	if usePos {
		face = posFace
		depth = dim - layer
		baseTurn = -1
	}
	prefix := ""
	if depth > 1 {
		prefix = fmt.Sprintf("%d", depth)
	}
	suffix := ""
	if turns == 2 || turns == -2 {
		suffix = "2"
	} else if turns != baseTurn {
		suffix = "'"
	}
	return prefix + face + suffix
}

func NewSystem(dim int) *System {
	geom := FaceletGeometry(dim)
	idx := make(map[[6]int]int)
	for i, g := range geom {
		idx[g] = i
	}
	s := &System{Dim: dim, Geom: geom, Index: idx, Moves: make(map[string]Move)}
	high := dim - 1
	for _, axis := range []rune{'x', 'y', 'z'} {
		for layer := 0; layer < dim; layer++ {
			axisIdx := int(axis - 'x')
			centeredLayer := 2*layer - high
			for _, turns := range []int{-1, 1, 2} {
				perm := make([]int, len(geom))
				for i := range perm {
					perm[i] = i
				}
				for src, entry := range geom {
					pos := [3]int{entry[0], entry[1], entry[2]}
					norm := [3]int{entry[3], entry[4], entry[5]}
					if pos[axisIdx] != centeredLayer {
						continue
					}
					nextPos := rotateVector(pos, axis, turns)
					nextNorm := rotateVector(norm, axis, turns)
					dst := idx[[6]int{nextPos[0], nextPos[1], nextPos[2], nextNorm[0], nextNorm[1], nextNorm[2]}]
					perm[dst] = src
				}
				not := moveNotation(dim, axis, layer, turns)
				s.Moves[not] = Move{Axis: axis, Layer: layer, Turns: turns, Notation: not, Perm: perm}
			}
		}
	}
	// Wide moves: Uw, Rw, Fw, Dw, Lw, Bw
	for _, f := range []string{"U", "R", "F", "D", "L", "B"} {
		for _, suf := range []string{"", "'", "2"} {
			m1 := s.Moves[f+suf]
			m2 := s.Moves["2"+f+suf]
			if len(m1.Perm) == 0 || len(m2.Perm) == 0 {
				continue
			}
			wPerm := make([]int, len(geom))
			for i := range wPerm {
				wPerm[i] = m1.Perm[m2.Perm[i]]
			}
			not := f + "w" + suf
			s.Moves[not] = Move{Notation: not, Perm: wPerm}
			s.Moves["2"+not] = Move{Notation: "2" + not, Perm: wPerm}
		}
	}
	// Whole cube rotations x, y, z (aligned with R, U, F where baseTurn is -1)
	for _, axis := range []rune{'x', 'y', 'z'} {
		axisStr := string(axis)
		for _, turns := range []int{1, -1, 2} {
			suf := ""
			if turns == 2 || turns == -2 {
				suf = "2"
			} else if turns == 1 {
				suf = "'"
			}
			not := axisStr + suf
			perm := make([]int, len(geom))
			for i := range perm {
				perm[i] = i
			}
			for src, entry := range geom {
				pos := [3]int{entry[0], entry[1], entry[2]}
				norm := [3]int{entry[3], entry[4], entry[5]}
				nextPos := rotateVector(pos, axis, turns)
				nextNorm := rotateVector(norm, axis, turns)
				dst := idx[[6]int{nextPos[0], nextPos[1], nextPos[2], nextNorm[0], nextNorm[1], nextNorm[2]}]
				perm[dst] = src
			}
			s.Moves[not] = Move{Notation: not, Perm: perm}
		}
	}
	return s
}

type CubeState struct {
	Dim   int
	State []byte
}

func NewSolvedState(dim int) *CubeState {
	var b strings.Builder
	for _, f := range "URFDLB" {
		for i := 0; i < dim*dim; i++ {
			b.WriteRune(f)
		}
	}
	return &CubeState{Dim: dim, State: []byte(b.String())}
}

func (c *CubeState) Clone() *CubeState {
	st := make([]byte, len(c.State))
	copy(st, c.State)
	return &CubeState{Dim: c.Dim, State: st}
}

func (c *CubeState) Apply(perm []int) {
	next := make([]byte, len(c.State))
	for dst, src := range perm {
		next[dst] = c.State[src]
	}
	c.State = next
}

func (s *System) ApplySeq(c *CubeState, seq string) {
	for _, tok := range strings.Fields(seq) {
		m, ok := s.Moves[tok]
		if !ok {
			panic(fmt.Sprintf("unknown move notation: %s", tok))
		}
		c.Apply(m.Perm)
	}
}

func (s *System) InvertSeq(seq string) string {
	toks := strings.Fields(seq)
	var inv []string
	for i := len(toks) - 1; i >= 0; i-- {
		t := toks[i]
		if strings.HasSuffix(t, "2") {
			inv = append(inv, t)
		} else if strings.HasSuffix(t, "'") {
			inv = append(inv, strings.TrimSuffix(t, "'"))
		} else {
			inv = append(inv, t+"'")
		}
	}
	return strings.Join(inv, " ")
}

func IsUniformlySolved(c *CubeState) bool {
	dim := c.Dim
	faceSize := dim * dim
	for i := 0; i < 6; i++ {
		first := c.State[i*faceSize]
		for j := 1; j < faceSize; j++ {
			if c.State[i*faceSize+j] != first {
				return false
			}
		}
	}
	return true
}

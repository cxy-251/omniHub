package games

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"errors"
	"io"
)

// Windows 资源类型
const (
	rtIcon      = 3
	rtGroupIcon = 14
)

// ExeIcon 从 Windows 可执行文件的资源里取出最大的一张图标。
// 新式图标本身就是 PNG，直接返回；老式 BMP 图标封装成单图 .ico 返回（Qt 的 ico 插件可解码）。
// 返回值第二项为 MIME 类型。
func ExeIcon(path string) ([]byte, string, error) {
	f, err := pe.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	sec := f.Section(".rsrc")
	if sec == nil {
		return nil, "", errors.New("no resource section")
	}
	if sec.Size > 64<<20 {
		return nil, "", errors.New("resource section too large")
	}
	data, err := io.ReadAll(io.LimitReader(sec.Open(), int64(sec.Size)))
	if err != nil {
		return nil, "", err
	}
	r := resources{data: data, base: sec.VirtualAddress}

	group, err := r.firstLeaf(rtGroupIcon)
	if err != nil {
		return nil, "", err
	}
	if len(group) < 6 {
		return nil, "", errors.New("invalid icon group")
	}
	count := int(binary.LittleEndian.Uint16(group[4:]))
	bestID, bestScore := -1, -1
	var best []byte
	for i := 0; i < count && 6+(i+1)*14 <= len(group); i++ {
		e := group[6+i*14:]
		w := int(e[0])
		if w == 0 {
			w = 256
		}
		bits := int(binary.LittleEndian.Uint16(e[6:]))
		score := w*100 + bits
		if score > bestScore {
			bestScore, bestID, best = score, int(binary.LittleEndian.Uint16(e[12:])), e[:12]
		}
	}
	if bestID < 0 {
		return nil, "", errors.New("empty icon group")
	}
	img, err := r.leaf(rtIcon, bestID)
	if err != nil {
		return nil, "", err
	}
	if bytes.HasPrefix(img, []byte("\x89PNG")) {
		return img, "image/png", nil
	}
	// ICONDIR + 单个 ICONDIRENTRY（16 字节，最后 4 字节为数据偏移）+ DIB 数据
	var ico bytes.Buffer
	ico.Write([]byte{0, 0, 1, 0, 1, 0})
	ico.Write(best[:8])
	binary.Write(&ico, binary.LittleEndian, uint32(len(img)))
	binary.Write(&ico, binary.LittleEndian, uint32(22))
	ico.Write(img)
	return ico.Bytes(), "image/x-icon", nil
}

// resources 解析 .rsrc 的三级目录：类型 → 名称/ID → 语言。
type resources struct {
	data []byte
	base uint32 // 节的虚拟地址（数据项里的 RVA 要减去它）
}

type resEntry struct {
	id     uint32
	offset uint32
	isDir  bool
}

func (r resources) dir(offset uint32) ([]resEntry, error) {
	if int(offset)+16 > len(r.data) {
		return nil, errors.New("resource directory out of range")
	}
	d := r.data[offset:]
	n := int(binary.LittleEndian.Uint16(d[12:])) + int(binary.LittleEndian.Uint16(d[14:]))
	if n > 4096 || int(offset)+16+n*8 > len(r.data) {
		return nil, errors.New("resource directory too large")
	}
	out := make([]resEntry, n)
	for i := range out {
		e := d[16+i*8:]
		name := binary.LittleEndian.Uint32(e)
		off := binary.LittleEndian.Uint32(e[4:])
		out[i] = resEntry{id: name, offset: off & 0x7fffffff, isDir: off&0x80000000 != 0}
	}
	return out, nil
}

// leaf 返回指定类型、ID 的第一个语言版本的数据；id < 0 表示取该类型的第一个。
func (r resources) leaf(typ, id int) ([]byte, error) {
	types, err := r.dir(0)
	if err != nil {
		return nil, err
	}
	for _, t := range types {
		if t.id != uint32(typ) || !t.isDir {
			continue
		}
		names, err := r.dir(t.offset)
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			if (id >= 0 && n.id != uint32(id)) || !n.isDir {
				continue
			}
			langs, err := r.dir(n.offset)
			if err != nil || len(langs) == 0 || langs[0].isDir {
				continue
			}
			de := langs[0].offset
			if int(de)+8 > len(r.data) {
				return nil, errors.New("resource data entry out of range")
			}
			rva := binary.LittleEndian.Uint32(r.data[de:])
			size := binary.LittleEndian.Uint32(r.data[de+4:])
			start := int64(rva) - int64(r.base)
			if start < 0 || start+int64(size) > int64(len(r.data)) || size > 16<<20 {
				return nil, errors.New("resource data out of range")
			}
			return r.data[start : start+int64(size)], nil
		}
	}
	return nil, errors.New("resource not found")
}

func (r resources) firstLeaf(typ int) ([]byte, error) { return r.leaf(typ, -1) }

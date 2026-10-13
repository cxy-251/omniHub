package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func tagSize(n int) []byte {
	return []byte{byte(n>>21) & 127, byte(n>>14) & 127, byte(n>>7) & 127, byte(n) & 127}
}
func tagFrame(id string, data []byte, version byte) []byte {
	header := []byte(id)
	if version == 2 {
		header = append(header, byte(len(data)>>16), byte(len(data)>>8), byte(len(data)))
	} else {
		size := make([]byte, 4)
		binary.BigEndian.PutUint32(size, uint32(len(data)))
		if version == 4 {
			size = tagSize(len(data))
		}
		header = append(header, size...)
		header = append(header, 0, 0)
	}
	return append(header, data...)
}
func tagBox(kind string, data []byte) []byte {
	header := make([]byte, 8)
	binary.BigEndian.PutUint32(header, uint32(len(data)+8))
	copy(header[4:], kind)
	return append(header, data...)
}

func TestAudioMetadataID3VersionsAndEmbeddedCover(t *testing.T) {
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	for _, version := range []byte{2, 3, 4} {
		titleID, artistID, pictureID := "TIT2", "TPE1", "APIC"
		if version == 2 {
			titleID, artistID, pictureID = "TT2", "TP1", "PIC"
		}
		body := tagFrame(titleID, append([]byte{3}, []byte("中文标题")...), version)
		body = append(body, tagFrame(artistID, []byte{1, 255, 254, 'A', 0, 0, 0}, version)...)
		prefix := append([]byte{0}, []byte("image/png\x00\x03\x00")...)
		if version == 2 {
			prefix = []byte{0, 'P', 'N', 'G', 3, 0}
		}
		body = append(body, tagFrame(pictureID, append(prefix, picture.Bytes()...), version)...)
		data := append([]byte{'I', 'D', '3', version, 0, 0}, tagSize(len(body))...)
		data = append(data, body...)
		f, err := os.Create(filepath.Join(t.TempDir(), "audio.mp3"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write(data); err != nil {
			t.Fatal(err)
		}
		result, err := ReadMetadata(context.Background(), f)
		if closeErr := f.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		if err != nil || result.Title != "中文标题" || result.Artist != "A" || !result.HasCover || !bytes.Equal(result.Picture, picture.Bytes()) {
			t.Fatalf("ID3 v%d: %+v %v", version, result, err)
		}
	}
}

func TestMP4TrailingMetadataSkipsLargeMediaPayload(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "audio.m4a"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	prefix := tagBox("ftyp", []byte("M4A "))
	if _, err = f.Write(prefix); err != nil {
		t.Fatal(err)
	}
	mediaSize := int64(2 << 30)
	media := make([]byte, 8)
	binary.BigEndian.PutUint32(media, uint32(mediaSize))
	copy(media[4:], "mdat")
	if _, err = f.Write(media); err != nil {
		t.Fatal(err)
	}
	label := append([]byte{0, 0, 0, 1, 0, 0, 0, 0}, []byte("尾部标题")...)
	metadata := tagBox("moov", tagBox("udta", tagBox("meta", append(make([]byte, 4), tagBox("ilst", tagBox("\xa9nam", tagBox("data", label)))...))))
	offset := int64(len(prefix)) + mediaSize
	if _, err = f.WriteAt(metadata, offset); err != nil {
		t.Fatal(err)
	}
	result, err := ReadMetadata(context.Background(), f)
	if err != nil || result.Title != "尾部标题" {
		t.Fatalf("trailing MP4: %+v %v", result, err)
	}
	// Count physical reads independently of the sparse file's apparent size.
	r := &tagInput{ctx: context.Background(), file: f, size: offset + int64(len(metadata)), remaining: metadataBudget}
	if err := r.mp4(0, r.size, 0, "", &Metadata{}); err != nil {
		t.Fatal(err)
	}
	if metadataBudget-r.remaining > 1024 {
		t.Fatal("metadata parser scanned media bytes")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadMetadata(ctx, f); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestMalformedAudioMetadataBounds(t *testing.T) {
	for _, data := range [][]byte{
		append(append([]byte{'I', 'D', '3', 2, 0, 0}, tagSize(6)...), []byte{'T', 'T', '2', 255, 255, 255}...),
		append(append([]byte{'I', 'D', '3', 3, 0, 0}, tagSize(10)...), []byte{'T', 'I', 'T', '2', 255, 255, 255, 255, 0, 0}...),
		append(tagBox("ftyp", []byte("M4A ")), []byte{0, 0, 0, 7, 'm', 'o', 'o', 'v'}...),
	} {
		f, err := os.Create(filepath.Join(t.TempDir(), "bad.audio"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write(data); err != nil {
			t.Fatal(err)
		}
		if _, err = ReadMetadata(context.Background(), f); err == nil {
			t.Fatal("malformed metadata accepted")
		}
		if err = f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Create(filepath.Join(t.TempDir(), "large.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	header := append([]byte{'I', 'D', '3', 3, 0, 0}, tagSize(metadataBudget+1)...)
	if _, err = f.Write(header); err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(metadataBudget + 11); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadMetadata(context.Background(), f); !errors.Is(err, ErrMetadataCapacity) {
		t.Fatalf("oversized tag: %v", err)
	}
}

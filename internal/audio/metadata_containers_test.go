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

func commentFields(fields ...string) []byte {
	data := make([]byte, 8)
	binary.LittleEndian.PutUint32(data[4:], uint32(len(fields)))
	for _, field := range fields {
		size := make([]byte, 4)
		binary.LittleEndian.PutUint32(size, uint32(len(field)))
		data = append(data, size...)
		data = append(data, field...)
	}
	return data
}
func flacBlock(kind byte, data []byte) []byte {
	return append([]byte{kind, byte(len(data) >> 16), byte(len(data) >> 8), byte(len(data))}, data...)
}
func riffChunk(kind string, data []byte) []byte {
	header := make([]byte, 8)
	copy(header, kind)
	binary.LittleEndian.PutUint32(header[4:], uint32(len(data)))
	out := append(header, data...)
	if len(data)%2 != 0 {
		out = append(out, 0)
	}
	return out
}
func readContainer(t *testing.T, data []byte) (Metadata, error) {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "audio"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(data); err != nil {
		t.Fatal(err)
	}
	value, readErr := ReadMetadata(context.Background(), f)
	return value, errors.Join(readErr, f.Close())
}

func TestFLACCommentsPictureAndExternalPictureBoundary(t *testing.T) {
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	lengthField := func(data []byte) []byte {
		size := make([]byte, 4)
		binary.BigEndian.PutUint32(size, uint32(len(data)))
		return append(size, data...)
	}
	body := []byte{0, 0, 0, 3}
	body = append(body, lengthField([]byte("image/png"))...)
	body = append(body, lengthField([]byte("封面"))...)
	body = append(body, make([]byte, 16)...)
	body = append(body, lengthField(picture.Bytes())...)
	data := append([]byte("fLaC"), flacBlock(0, make([]byte, 34))...)
	data = append(data, flacBlock(4, commentFields("TITLE=标题", "Artist=作者", "ALBUM=专辑"))...)
	data = append(data, flacBlock(0x86, body)...)
	result, err := readContainer(t, data)
	if err != nil || result.Title != "标题" || result.Artist != "作者" || result.Album != "专辑" || !result.HasCover || !bytes.Equal(result.Picture, picture.Bytes()) {
		t.Fatalf("FLAC metadata: %+v %v", result, err)
	}
	external := append([]byte{0, 0, 0, 3}, lengthField([]byte("-->"))...)
	external = append(external, lengthField(nil)...)
	external = append(external, make([]byte, 16)...)
	external = append(external, lengthField([]byte("https://invalid.example/cover"))...)
	result = Metadata{}
	if err := flacPicture(external, &result); err != nil || len(result.Picture) != 0 {
		t.Fatalf("external picture exposed: %v", err)
	}
}

func TestWAVInfoAfterLargeAudioPayloadDoesNotScanIt(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "large.wav"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info := []byte("INFO")
	info = append(info, riffChunk("INAM", []byte("标题\x00"))...)
	info = append(info, riffChunk("IART", []byte("artist\x00"))...)
	info = append(info, riffChunk("IPRD", []byte("album\x00"))...)
	tail := riffChunk("LIST", info)
	payload := int64(2 << 30)
	header := make([]byte, 20)
	copy(header, "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(12+payload+int64(len(tail))))
	copy(header[8:], "WAVEdata")
	binary.LittleEndian.PutUint32(header[16:], uint32(payload))
	if _, err = f.Write(header); err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteAt(tail, 20+payload); err != nil {
		t.Fatal(err)
	}
	result, err := ReadMetadata(context.Background(), f)
	if err != nil || result.Title != "标题" || result.Artist != "artist" || result.Album != "album" {
		t.Fatalf("WAV metadata: %+v %v", result, err)
	}
	r := &tagInput{ctx: context.Background(), file: f, size: 20 + payload + int64(len(tail)), remaining: metadataBudget}
	if err := r.wav(&Metadata{}); err != nil {
		t.Fatal(err)
	}
	if metadataBudget-r.remaining > 1024 {
		t.Fatal("WAV audio payload scanned")
	}
}

func TestContainerMetadataRejectsMalformedLengths(t *testing.T) {
	validFLAC := append([]byte("fLaC"), flacBlock(0, make([]byte, 34))...)
	malformedComment := commentFields("TITLE=valid")
	binary.LittleEndian.PutUint32(malformedComment[8:], 0xffffffff)
	for _, data := range [][]byte{
		append([]byte("fLaC"), flacBlock(0x84, commentFields("TITLE=missing streaminfo"))...),
		append(append([]byte(nil), validFLAC...), flacBlock(0x84, malformedComment)...),
		append(append([]byte(nil), validFLAC...), flacBlock(0x84, append(commentFields("TITLE=framing not allowed"), 1))...),
		append(append([]byte(nil), validFLAC...), flacBlock(0x86, []byte{0, 0, 0, 3, 255, 255, 255, 255})...),
		[]byte("RIFF\xff\xff\xff\xffWAVE"),
	} {
		if _, err := readContainer(t, data); err == nil {
			t.Fatal("malformed container metadata accepted")
		}
	}
	result := Metadata{}
	if err := vorbisComments(commentFields("TITLE=\xff"), &result, "flac"); err == nil {
		t.Fatal("invalid UTF-8 comment accepted")
	}
}

func TestWAVEmbeddedID3Labels(t *testing.T) {
	body := tagFrame("TIT2", append([]byte{3}, []byte("WAV 标题")...), 3)
	id3 := append([]byte{'I', 'D', '3', 3, 0, 0}, tagSize(len(body))...)
	id3 = append(id3, body...)
	chunks := riffChunk("id3 ", id3)
	header := make([]byte, 12)
	copy(header, "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(4+len(chunks)))
	copy(header[8:], "WAVE")
	result, err := readContainer(t, append(header, chunks...))
	if err != nil || result.Title != "WAV 标题" {
		t.Fatalf("WAVE ID3: %+v %v", result, err)
	}
}

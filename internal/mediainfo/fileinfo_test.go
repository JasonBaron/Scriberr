package mediainfo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.wav")
	if err := os.WriteFile(p, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, n, err := HashFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if h != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" || n != 3 {
		t.Errorf("got %s %d", h, n)
	}
}

func TestParseProbe(t *testing.T) {
	raw := `{"streams":[{"codec_type":"video","codec_name":"h264"},{"codec_type":"audio","codec_name":"aac","codec_long_name":"AAC (Advanced Audio Coding)","sample_rate":"48000","channels":1,"channel_layout":"mono","tags":{"handler_name":"Core Media Audio","creation_time":"stream"}}],
	"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","format_long_name":"QuickTime / MOV","duration":"300.012","bit_rate":"64021","tags":{"creation_time":"2026-09-14T13:05:22.000000Z","encoder":"com.apple.VoiceMemos"}}}`
	info, err := ParseProbe([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if info.Codec != "AAC (Advanced Audio Coding)" || info.SampleRate != 48000 || info.Channels != 1 || info.Duration != 300.012 || info.BitRate != 64021 {
		t.Errorf("info = %+v", info)
	}
	if info.Tags["creation_time"] != "2026-09-14T13:05:22.000000Z" || info.Tags["handler_name"] != "Core Media Audio" || info.Tags["encoder"] != "com.apple.VoiceMemos" {
		t.Errorf("tags = %v", info.Tags)
	}
}

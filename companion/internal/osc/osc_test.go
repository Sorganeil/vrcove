package osc

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEncodeMatchesTheWireFormat(t *testing.T) {
	packet, err := Encode(Message{"/chatbox/input", []any{"Hello", true, false}})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("/chatbox/input\x00\x00,sTF\x00\x00\x00\x00Hello\x00\x00\x00")
	if !bytes.Equal(packet, want) {
		t.Fatalf("packet=%q want=%q", packet, want)
	}
	if _, err := Encode(Message{"/a", []any{1.5}}); err == nil {
		t.Fatal("accepted a float64")
	}
}

func TestDecodeRoundTrip(t *testing.T) {
	packet, err := Encode(Message{"/avatar/parameters/Mix", []any{int32(-7), float32(0.25), "text", true, false}})
	if err != nil {
		t.Fatal(err)
	}
	messages, err := Decode(packet)
	if err != nil || len(messages) != 1 {
		t.Fatalf("decode: %v %v", messages, err)
	}
	args := messages[0].Args
	if messages[0].Address != "/avatar/parameters/Mix" || args[0] != int32(-7) || args[1] != float32(0.25) || args[2] != "text" || args[3] != true || args[4] != false {
		t.Fatalf("message=%+v", messages[0])
	}
}

func bundle(elements ...[]byte) []byte {
	var b bytes.Buffer
	b.WriteString("#bundle\x00")
	b.Write(make([]byte, 8))
	for _, element := range elements {
		binary.Write(&b, binary.BigEndian, uint32(len(element)))
		b.Write(element)
	}
	return b.Bytes()
}

func TestDecodeBundles(t *testing.T) {
	a, _ := Encode(Message{"/a", []any{true}})
	b, _ := Encode(Message{"/b", []any{int32(2)}})
	messages, err := Decode(bundle(a, bundle(b)))
	if err != nil || len(messages) != 2 || messages[1].Address != "/b" {
		t.Fatalf("bundle: %+v %v", messages, err)
	}
}

func TestDecodeRejectsMalformedPackets(t *testing.T) {
	nested, _ := Encode(Message{"/a", nil})
	for range maxBundleDepth + 2 {
		nested = bundle(nested)
	}
	oversized := append(bundle(), 0xff, 0xff, 0xff, 0xff)
	for name, packet := range map[string][]byte{
		"empty": {}, "no address": []byte("hello\x00\x00\x00"), "unterminated": []byte("/abc"),
		"short int": []byte("/a\x00\x00,i\x00\x00\x00"), "unknown tag": []byte("/a\x00\x00,x\x00\x00"),
		"short bundle": []byte("#bundle\x00"), "oversized element": oversized, "deep nesting": nested,
		"short string": []byte("/a\x00\x00,s\x00\x00ab\x00"),
	} {
		if messages, err := Decode(packet); err == nil {
			t.Errorf("%s accepted: %+v", name, messages)
		}
	}
}

func TestListenReceivesAndStops(t *testing.T) {
	probe, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan Message, 1)
	if err := Listen(ctx, port, func(m Message) { got <- m }); err != nil {
		t.Fatal(err)
	}
	if err := Listen(context.Background(), port, func(Message) {}); err == nil {
		t.Error("two listeners shared a port")
	}
	if err := Send(port, []byte("garbage")); err != nil {
		t.Fatal(err)
	}
	packet, _ := Encode(Message{"/avatar/parameters/AFK", []any{true}})
	if err := Send(port, packet); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-got:
		if m.Address != "/avatar/parameters/AFK" {
			t.Fatalf("message=%+v", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing received")
	}
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := Listen(context.Background(), port, func(Message) {})
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("port stayed bound: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

const testAvatar = "avtr_00000000-0000-0000-0000-000000000001"
const avatarConfig = "\xef\xbb\xbf" + `{"id":"` + testAvatar + `","name":"Test","parameters":[
	{"name":"Glasses","input":{"address":"/avatar/parameters/Glasses","type":"Bool"},"output":{"address":"/avatar/parameters/Glasses","type":"Bool"}},
	{"name":"Hue","input":{"address":"/avatar/parameters/Hue","type":"Float"},"output":{"address":"/avatar/parameters/Hue","type":"Float"}},
	{"name":"Velocity","output":{"address":"/avatar/parameters/Velocity","type":"Float"}},
	{"name":"Odd","output":{"address":"/avatar/parameters/Odd","type":"String"}}]}`

func writeAvatar(t *testing.T, folder, user, id, body string, age time.Duration) {
	t.Helper()
	dir := filepath.Join(folder, user, "Avatars")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, id+".json")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(file, when, when); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAvatar(t *testing.T) {
	folder := t.TempDir()
	writeAvatar(t, folder, "usr_a", testAvatar, avatarConfig, time.Minute)
	avatar, err := LoadAvatar(folder, testAvatar)
	if err != nil {
		t.Fatal(err)
	}
	if avatar.Name != "Test" || len(avatar.Parameters) != 3 {
		t.Fatalf("avatar=%+v", avatar)
	}
	if p := avatar.Parameters[2]; p.Name != "Velocity" || p.Input != "" || p.Type != "Float" {
		t.Fatalf("output-only parameter=%+v", p)
	}
	if _, err := LoadAvatar(folder, "avtr_00000000-0000-0000-0000-000000000002"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing avatar: %v", err)
	}
	for _, id := range []string{"../../secrets", "avtr_*", ""} {
		if _, err := LoadAvatar(folder, id); err == nil || errors.Is(err, fs.ErrNotExist) {
			t.Errorf("accepted ID %q: %v", id, err)
		}
	}
	// The newest config wins, across accounts and avatars.
	second := "avtr_00000000-0000-0000-0000-000000000002"
	writeAvatar(t, folder, "usr_b", second, `{"id":"`+second+`","name":"Newer"}`, time.Second)
	latest, err := LatestAvatar(folder)
	if err != nil || latest.Name != "Newer" {
		t.Fatalf("latest=%+v %v", latest, err)
	}
	writeAvatar(t, folder, "usr_b", testAvatar, `{"id":"`+testAvatar+`","name":"Other account"}`, 0)
	if avatar, err := LoadAvatar(folder, testAvatar); err != nil || avatar.Name != "Other account" {
		t.Fatalf("newest account=%+v %v", avatar, err)
	}
	if _, err := LatestAvatar(t.TempDir()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("empty folder: %v", err)
	}
}

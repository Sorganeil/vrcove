// Package osc reads and writes the Open Sound Control messages VRChat uses,
// over UDP on this computer only.
package osc

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"
)

// maxBundleDepth stops a crafted packet from recursing without limit.
const maxBundleDepth = 4

// Message is an OSC address with arguments of type string, bool, int32 or float32.
type Message struct {
	Address string
	Args    []any
}

// Encode builds the packet for m.
func Encode(m Message) ([]byte, error) {
	tags := strings.Builder{}
	tags.WriteByte(',')
	var body bytes.Buffer
	for _, arg := range m.Args {
		switch v := arg.(type) {
		case string:
			tags.WriteByte('s')
			writeString(&body, v)
		case bool:
			if v {
				tags.WriteByte('T')
			} else {
				tags.WriteByte('F')
			}
		case int32:
			tags.WriteByte('i')
			binary.Write(&body, binary.BigEndian, v)
		case float32:
			tags.WriteByte('f')
			binary.Write(&body, binary.BigEndian, v)
		default:
			return nil, fmt.Errorf("osc: unsupported argument %T", arg)
		}
	}
	var packet bytes.Buffer
	writeString(&packet, m.Address)
	writeString(&packet, tags.String())
	packet.Write(body.Bytes())
	return packet.Bytes(), nil
}

func writeString(b *bytes.Buffer, s string) {
	b.WriteString(s)
	b.WriteByte(0)
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
}

// Decode reads the messages in a packet, which may be a bundle.
func Decode(packet []byte) ([]Message, error) {
	return decode(packet, 0)
}

func decode(packet []byte, depth int) ([]Message, error) {
	if depth > maxBundleDepth {
		return nil, errors.New("osc: bundle nested too deeply")
	}
	if !bytes.HasPrefix(packet, []byte("#bundle\x00")) {
		m, err := decodeMessage(packet)
		if err != nil {
			return nil, err
		}
		return []Message{m}, nil
	}
	if len(packet) < 16 {
		return nil, errors.New("osc: short bundle")
	}
	var messages []Message
	for rest := packet[16:]; len(rest) > 0; {
		if len(rest) < 4 {
			return nil, errors.New("osc: short bundle element")
		}
		size := binary.BigEndian.Uint32(rest)
		rest = rest[4:]
		if uint64(size) > uint64(len(rest)) {
			return nil, errors.New("osc: bundle element overruns packet")
		}
		inner, err := decode(rest[:size], depth+1)
		if err != nil {
			return nil, err
		}
		messages = append(messages, inner...)
		rest = rest[size:]
	}
	return messages, nil
}

func decodeMessage(packet []byte) (Message, error) {
	address, rest, err := readString(packet)
	if err != nil || !strings.HasPrefix(address, "/") {
		return Message{}, errors.New("osc: invalid address")
	}
	m := Message{Address: address}
	if len(rest) == 0 {
		return m, nil
	}
	tags, rest, err := readString(rest)
	if err != nil || !strings.HasPrefix(tags, ",") {
		return Message{}, errors.New("osc: invalid type tags")
	}
	for _, tag := range tags[1:] {
		switch tag {
		case 'T':
			m.Args = append(m.Args, true)
		case 'F':
			m.Args = append(m.Args, false)
		case 's':
			var s string
			if s, rest, err = readString(rest); err != nil {
				return Message{}, err
			}
			m.Args = append(m.Args, s)
		case 'i', 'f':
			if len(rest) < 4 {
				return Message{}, errors.New("osc: short argument")
			}
			bits := binary.BigEndian.Uint32(rest)
			rest = rest[4:]
			if tag == 'i' {
				m.Args = append(m.Args, int32(bits))
			} else {
				m.Args = append(m.Args, math.Float32frombits(bits))
			}
		default:
			return Message{}, fmt.Errorf("osc: unsupported type %q", tag)
		}
	}
	return m, nil
}

// readString reads one NUL-terminated string padded to four bytes.
func readString(b []byte) (string, []byte, error) {
	end := bytes.IndexByte(b, 0)
	if end < 0 {
		return "", nil, errors.New("osc: unterminated string")
	}
	next := (end + 4) &^ 3
	if next > len(b) {
		return "", nil, errors.New("osc: short string padding")
	}
	return string(b[:end]), b[next:], nil
}

// Send writes a packet to port on this computer.
func Send(port int, packet []byte) error {
	c, err := net.DialTimeout("udp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetWriteDeadline(time.Now().Add(time.Second))
	_, err = c.Write(packet)
	return err
}

// Listen receives messages on port until ctx ends. Only this computer can
// reach the socket. It returns once the port is bound, or an error if another
// app holds it. Packets that aren't valid OSC are ignored.
func Listen(ctx context.Context, port int, handle func(Message)) error {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	go func() {
		defer stop()
		defer conn.Close()
		buffer := make([]byte, 65535)
		for {
			n, err := conn.Read(buffer)
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}
			messages, err := Decode(buffer[:n])
			if err != nil {
				continue
			}
			for _, m := range messages {
				handle(m)
			}
		}
	}()
	return nil
}

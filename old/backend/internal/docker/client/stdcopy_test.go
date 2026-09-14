package client

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

// frame 构造一个多路复用帧。
func frame(st StreamType, data string) []byte {
	b := make([]byte, frameHeaderSize+len(data))
	b[0] = byte(st)
	binary.BigEndian.PutUint32(b[4:frameHeaderSize], uint32(len(data)))
	copy(b[frameHeaderSize:], data)
	return b
}

// collect 读空一个 FrameReader,拷贝每帧内容后返回。
func collect(t *testing.T, r io.Reader) []struct {
	St   StreamType
	Data string
} {
	t.Helper()
	fr := NewFrameReader(r)
	var got []struct {
		St   StreamType
		Data string
	}
	for {
		st, payload, err := fr.Next()
		if err == io.EOF {
			return got
		}
		if err != nil {
			t.Fatalf("Next() 意外错误: %v", err)
		}
		got = append(got, struct {
			St   StreamType
			Data string
		}{st, string(payload)}) // 立即拷贝——缓冲会被复用
	}
}

func TestFrameReader_MixedStreams(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(frame(StreamStdout, "hello"))
	buf.Write(frame(StreamStderr, "oops"))
	buf.Write(frame(StreamStdout, "world"))

	got := collect(t, &buf)
	if len(got) != 3 {
		t.Fatalf("帧数 = %d, want 3", len(got))
	}
	if got[0].St != StreamStdout || got[0].Data != "hello" {
		t.Errorf("帧 0 = (%v, %q), want (stdout, hello)", got[0].St, got[0].Data)
	}
	if got[1].St != StreamStderr || got[1].Data != "oops" {
		t.Errorf("帧 1 = (%v, %q), want (stderr, oops)", got[1].St, got[1].Data)
	}
	if got[2].St != StreamStdout || got[2].Data != "world" {
		t.Errorf("帧 2 = (%v, %q), want (stdout, world)", got[2].St, got[2].Data)
	}
}

// TestFrameReader_AcrossReadBoundary 是本文件最重要的用例:
// 每次 Read 只返回 1 字节,帧头与 payload 都会被切碎,
// 任何「假设一次 Read 就能拿到完整帧头」的实现都会在此暴露。
func TestFrameReader_AcrossReadBoundary(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(frame(StreamStdout, "hello"))
	buf.Write(frame(StreamStderr, "a longer stderr payload spanning reads"))
	buf.Write(frame(StreamStdout, "x"))

	got := collect(t, iotest.OneByteReader(&buf))
	want := []string{"hello", "a longer stderr payload spanning reads", "x"}
	if len(got) != len(want) {
		t.Fatalf("帧数 = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Data != w {
			t.Errorf("帧 %d = %q, want %q", i, got[i].Data, w)
		}
	}
	if got[1].St != StreamStderr {
		t.Errorf("帧 1 流类型 = %v, want stderr", got[1].St)
	}
}

// TestFrameReader_HalfReader 用另一种切分方式(每次半个缓冲)再验一遍。
func TestFrameReader_HalfReader(t *testing.T) {
	var buf bytes.Buffer
	for i := 0; i < 20; i++ {
		buf.Write(frame(StreamStdout, strings.Repeat("ab", i+1)))
	}
	got := collect(t, iotest.HalfReader(&buf))
	if len(got) != 20 {
		t.Fatalf("帧数 = %d, want 20", len(got))
	}
	for i, g := range got {
		want := strings.Repeat("ab", i+1)
		if g.Data != want {
			t.Errorf("帧 %d = %q, want %q", i, g.Data, want)
		}
	}
}

func TestFrameReader_EmptyPayload(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(frame(StreamStdout, ""))
	buf.Write(frame(StreamStdout, "after empty"))

	got := collect(t, &buf)
	if len(got) != 2 {
		t.Fatalf("帧数 = %d, want 2(空 payload 帧不应被吞掉)", len(got))
	}
	if got[0].Data != "" {
		t.Errorf("帧 0 = %q, want 空", got[0].Data)
	}
	if got[1].Data != "after empty" {
		t.Errorf("帧 1 = %q", got[1].Data)
	}
}

func TestFrameReader_CleanEOF(t *testing.T) {
	// 恰好在帧边界结束 —— 应为 io.EOF,表示日志完整
	fr := NewFrameReader(bytes.NewReader(frame(StreamStdout, "done")))
	if _, _, err := fr.Next(); err != nil {
		t.Fatalf("首帧不应出错: %v", err)
	}
	_, _, err := fr.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("err = %v, want io.EOF", err)
	}
}

func TestFrameReader_TruncatedHeader(t *testing.T) {
	// 帧头只有 3 字节 —— 截断,应与「干净结束」区分开
	fr := NewFrameReader(bytes.NewReader([]byte{1, 0, 0}))
	_, _, err := fr.Next()
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("err = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestFrameReader_TruncatedPayload(t *testing.T) {
	// 帧头声明 100 字节,实际只有 4 字节
	f := frame(StreamStdout, strings.Repeat("x", 100))
	fr := NewFrameReader(bytes.NewReader(f[:frameHeaderSize+4]))
	_, _, err := fr.Next()
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("err = %v, want io.ErrUnexpectedEOF", err)
	}
}

// TestFrameReader_OversizeFrame 覆盖「TTY 容器被误当作多路复用流」的场景:
// 正文字节被解析成帧头,长度字段会得出一个天文数字。
func TestFrameReader_OversizeFrame(t *testing.T) {
	hdr := make([]byte, frameHeaderSize)
	hdr[0] = 1
	binary.BigEndian.PutUint32(hdr[4:], maxFrameSize+1)
	fr := NewFrameReader(bytes.NewReader(hdr))

	_, _, err := fr.Next()
	if err == nil {
		t.Fatal("超长帧应报错")
	}
	if !strings.Contains(err.Error(), "帧长度异常") {
		t.Errorf("错误信息应提示帧长度异常与 TTY 可能性, got: %v", err)
	}
}

// TestFrameReader_BufferIsReused 固化「返回切片仅在下次 Next 前有效」这一契约,
// 防止后续有人误以为可以留存该切片。
func TestFrameReader_BufferIsReused(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(frame(StreamStdout, "AAAA"))
	buf.Write(frame(StreamStdout, "BBBB"))

	fr := NewFrameReader(&buf)
	_, p1, err := fr.Next()
	if err != nil {
		t.Fatal(err)
	}
	if string(p1) != "AAAA" {
		t.Fatalf("第一帧 = %q", p1)
	}
	if _, _, err = fr.Next(); err != nil {
		t.Fatal(err)
	}
	// 第二帧等长,必然复用同一底层数组 —— p1 的内容已变
	if string(p1) == "AAAA" {
		t.Error("缓冲未被复用,与 Next 的文档契约不符(调用方无需拷贝将成为隐性依赖)")
	}
}

func TestStdCopy(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(frame(StreamStdout, "out1"))
	buf.Write(frame(StreamStderr, "err1"))
	buf.Write(frame(StreamStdout, "out2"))

	var out, errw bytes.Buffer
	n, err := StdCopy(&out, &errw, iotest.OneByteReader(&buf))
	if err != nil {
		t.Fatalf("StdCopy 出错: %v", err)
	}
	if want := int64(12); n != want {
		t.Errorf("写出字节数 = %d, want %d", n, want)
	}
	if out.String() != "out1out2" {
		t.Errorf("stdout = %q, want %q", out.String(), "out1out2")
	}
	if errw.String() != "err1" {
		t.Errorf("stderr = %q, want %q", errw.String(), "err1")
	}
}

func TestStdCopy_NilWriterDiscards(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(frame(StreamStdout, "keep"))
	buf.Write(frame(StreamStderr, "drop"))

	var out bytes.Buffer
	if _, err := StdCopy(&out, nil, &buf); err != nil {
		t.Fatalf("stderr writer 为 nil 时不应出错: %v", err)
	}
	if out.String() != "keep" {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestStreamTypeString(t *testing.T) {
	cases := map[StreamType]string{
		StreamStdin:   "stdin",
		StreamStdout:  "stdout",
		StreamStderr:  "stderr",
		StreamType(9): "unknown(9)",
	}
	for st, want := range cases {
		if got := st.String(); got != want {
			t.Errorf("StreamType(%d).String() = %q, want %q", byte(st), got, want)
		}
	}
}

package client

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Docker 多路复用流(stdcopy)的帧格式:
//
//	字节 [0]     流类型:0=stdin 1=stdout 2=stderr
//	字节 [1..3]  保留(恒为 0)
//	字节 [4..7]  payload 长度(大端 uint32)
//	其后         payload
//
// 重要:仅**非 TTY** 容器的 logs/attach 输出采用此格式。TTY 容器直接返回
// 原始字节流、没有帧头——调用方须先读容器 inspect 的 Config.Tty 来区分,
// 用错会把帧头当正文显示(或把正文当帧头解析出天文数字长度)。
const (
	frameHeaderSize = 8

	// maxFrameSize 单帧 payload 上限,防御损坏流解析出的异常长度。
	// Docker 实际帧远小于此值(通常受 daemon 缓冲限制在 64KB 内)。
	maxFrameSize = 16 << 20
)

// StreamType 帧所属的标准流。
type StreamType byte

const (
	StreamStdin  StreamType = 0
	StreamStdout StreamType = 1
	StreamStderr StreamType = 2
)

func (s StreamType) String() string {
	switch s {
	case StreamStdin:
		return "stdin"
	case StreamStdout:
		return "stdout"
	case StreamStderr:
		return "stderr"
	default:
		return fmt.Sprintf("unknown(%d)", byte(s))
	}
}

// FrameReader 逐帧读取 Docker 多路复用流。
//
// 相比一次性分离到两个 Writer 的 StdCopy,逐帧读出能保留「这一段属于
// stdout 还是 stderr」的信息,便于日志 WebSocket 按流着色推送。
type FrameReader struct {
	r   io.Reader
	hdr [frameHeaderSize]byte
	buf []byte // 复用的 payload 缓冲,避免每帧分配
}

// NewFrameReader 包装一个多路复用流。
func NewFrameReader(r io.Reader) *FrameReader {
	return &FrameReader{r: r}
}

// Next 读取下一帧。
//
// 返回的 payload 切片在下次调用 Next 前有效(内部缓冲会被复用),
// 调用方若需留存必须自行拷贝。
//
// 流正常结束(恰好在帧边界)时返回 io.EOF;流在帧中途断开返回
// io.ErrUnexpectedEOF,两者语义不同,调用方可据此判断日志是否被截断。
func (fr *FrameReader) Next() (StreamType, []byte, error) {
	if _, err := io.ReadFull(fr.r, fr.hdr[:]); err != nil {
		// ReadFull 在一字节未读时返回 io.EOF,读到一半返回 io.ErrUnexpectedEOF
		return 0, nil, err
	}

	st := StreamType(fr.hdr[0])
	size := binary.BigEndian.Uint32(fr.hdr[4:frameHeaderSize])
	if size > maxFrameSize {
		return 0, nil, fmt.Errorf("docker: 帧长度异常 %d(上限 %d),流可能已损坏或容器为 TTY 模式", size, maxFrameSize)
	}
	if size == 0 {
		return st, nil, nil
	}

	if cap(fr.buf) < int(size) {
		fr.buf = make([]byte, size)
	}
	payload := fr.buf[:size]
	if _, err := io.ReadFull(fr.r, payload); err != nil {
		if err == io.EOF {
			// 头部已完整读出却读不到声明的 payload,属于截断而非正常结束
			err = io.ErrUnexpectedEOF
		}
		return 0, nil, err
	}
	return st, payload, nil
}

// StdCopy 将多路复用流分离写入 stdout / stderr 两个 Writer,返回写出的总字节数。
//
// 适用于「取全量日志」这类不需要区分片段归属的场景;需要逐帧归属信息时用 FrameReader。
// 流正常结束时返回 nil(而非 io.EOF)。
func StdCopy(dstout, dsterr io.Writer, src io.Reader) (int64, error) {
	fr := NewFrameReader(src)
	var written int64
	for {
		st, payload, err := fr.Next()
		if err == io.EOF {
			return written, nil
		}
		if err != nil {
			return written, err
		}
		if len(payload) == 0 {
			continue
		}

		var dst io.Writer
		switch st {
		case StreamStdout, StreamStdin:
			dst = dstout
		case StreamStderr:
			dst = dsterr
		default:
			return written, fmt.Errorf("docker: 未知流类型 %d", byte(st))
		}
		if dst == nil {
			written += int64(len(payload))
			continue
		}

		n, err := dst.Write(payload)
		written += int64(n)
		if err != nil {
			return written, err
		}
		if n != len(payload) {
			return written, io.ErrShortWrite
		}
	}
}

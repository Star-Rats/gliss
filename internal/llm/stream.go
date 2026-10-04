package llm

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// errDone 是 SSE 流读到 [DONE] 时的哨兵错误，用于从 scanSSE 中提前返回。
var errDone = errors.New("sse stream done")

// scanSSE 逐行读取 SSE 流，对每个非空 data 字段调用 onEvent。
// 事件处理函数返回 errDone 时正常终止。
func scanSSE(r io.Reader, onEvent func(data []byte) error) error {
	scanner := bufio.NewScanner(r)
	// SSE 单行可能携带很大的 JSON 分片（尤其 arguments 较长时），放宽 buffer 上限。
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if err := onEvent([]byte(data)); err != nil {
			if errors.Is(err, errDone) {
				return nil
			}
			return err
		}
	}
	return scanner.Err()
}

// truncateRunes 按字符（而非字节）截断字符串。
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

// readLimited 最多读取 n 字节。
func readLimited(r io.Reader, n int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, n))
}

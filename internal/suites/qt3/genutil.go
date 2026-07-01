package qt3

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// genErr wraps a fatal generation error raised deep in the catalog walk. The
// walk uses genFatalf/genFatal (panic) rather than threading an error through
// every helper; Suite.Fetch/Generate recover it via recoverGenErr.
type genErr struct{ err error }

func genFatalf(format string, args ...any) { panic(genErr{fmt.Errorf(format, args...)}) }

func genFatal(args ...any) { panic(genErr{errors.New(fmt.Sprint(args...))}) }

// recoverGenErr converts a genErr panic into *errp; any other panic re-panics.
func recoverGenErr(errp *error) {
	r := recover()
	if r == nil {
		return
	}
	if ge, ok := r.(genErr); ok {
		*errp = ge.err
		return
	}
	panic(r)
}

// copyAsset copies the file at src to dst, creating dst's parent directory if
// needed, and returns the destination close error too (a delayed write/flush
// failure only surfaces there).
func copyAsset(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // src is contained under the suite root
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst) //nolint:gosec // dst is contained under testdata/qt3ts
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// decodeXMLText decodes XML character/entity references and CDATA sections in s
// into their literal text.
func decodeXMLText(s string) string {
	var b strings.Builder
	for len(s) > 0 {
		if strings.HasPrefix(s, "<![CDATA[") {
			end := strings.Index(s, "]]>")
			if end < 0 {
				b.WriteString(s[len("<![CDATA["):])
				break
			}
			b.WriteString(s[len("<![CDATA["):end])
			s = s[end+len("]]>"):]
			continue
		}
		amp := strings.IndexByte(s, '&')
		cdata := strings.Index(s, "<![CDATA[")
		next := len(s)
		if amp >= 0 {
			next = amp
		}
		if cdata >= 0 && cdata < next {
			b.WriteString(s[:cdata])
			s = s[cdata:]
			continue
		}
		if amp < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:amp])
		s = s[amp:]
		semi := strings.IndexByte(s, ';')
		if semi < 0 {
			b.WriteString(s)
			break
		}
		ref := s[1:semi]
		s = s[semi+1:]
		switch {
		case strings.HasPrefix(ref, "#x") || strings.HasPrefix(ref, "#X"):
			if n, err := strconv.ParseInt(ref[2:], 16, 32); err == nil {
				b.WriteRune(rune(n))
			}
		case strings.HasPrefix(ref, "#"):
			if n, err := strconv.ParseInt(ref[1:], 10, 32); err == nil {
				b.WriteRune(rune(n))
			}
		default:
			switch ref {
			case "lt":
				b.WriteByte('<')
			case "gt":
				b.WriteByte('>')
			case "amp":
				b.WriteByte('&')
			case "apos":
				b.WriteByte('\'')
			case "quot":
				b.WriteByte('"')
			default:
				b.WriteByte('&')
				b.WriteString(ref)
				b.WriteByte(';')
			}
		}
	}
	return b.String()
}

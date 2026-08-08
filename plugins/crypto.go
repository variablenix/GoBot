package plugins

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"github.com/variablenix/GoBot/bot"
	"github.com/variablenix/GoBot/storage"
)

const cryptoInputLimit = 512

type Crypto struct{}

func (p *Crypto) Name() string { return "crypto" }
func (p *Crypto) Commands() []string {
	return []string{"hash", "md5", "sha1", "sha256", "sha512", "b64encode", "b64decode", "urlencode", "urldecode"}
}
func (p *Crypto) Help() string {
	return "!hash <md5|sha1|sha256|sha512> <text>; !b64encode/!b64decode <text>; !urlencode/!urldecode <text>"
}
func (p *Crypto) Init(_ bot.PluginConfig, _ *storage.DB) error { return nil }

func (p *Crypto) Handle(b *bot.Bot, m bot.Message) bool {
	cmd, arg, ok := bot.IsCommand(m, b.Config.CommandPrefix)
	if !ok {
		return false
	}
	cmd = strings.ToLower(cmd)
	if !cryptoCommand(cmd) {
		return false
	}
	arg = strings.TrimSpace(arg)
	if cmd == "hash" {
		fields := strings.Fields(arg)
		if len(fields) < 2 {
			b.Send(m.ReplyTarget(), "usage: !hash <md5|sha1|sha256|sha512> <text>")
			return true
		}
		cmd = fields[0]
		arg = strings.TrimSpace(arg[len(fields[0]):])
	}
	if len([]rune(arg)) > cryptoInputLimit {
		b.Send(m.ReplyTarget(), "crypto input is limited to 512 characters")
		return true
	}
	if arg == "" {
		b.Send(m.ReplyTarget(), "usage: provide text to process")
		return true
	}
	result, err := cryptoResult(strings.ToLower(cmd), arg)
	if err != nil {
		b.Send(m.ReplyTarget(), "unsupported crypto operation")
		return true
	}
	result = cleanExternalText(result)
	b.Send(m.ReplyTarget(), truncateRunes(result, 700))
	return true
}

func cryptoCommand(cmd string) bool {
	switch cmd {
	case "hash", "md5", "sha1", "sha256", "sha512", "b64encode", "b64decode", "urlencode", "urldecode":
		return true
	default:
		return false
	}
}

func cryptoResult(command, input string) (string, error) {
	switch command {
	case "md5":
		sum := md5.Sum([]byte(input))
		return hex.EncodeToString(sum[:]), nil
	case "sha1":
		sum := sha1.Sum([]byte(input))
		return hex.EncodeToString(sum[:]), nil
	case "sha256":
		sum := sha256.Sum256([]byte(input))
		return hex.EncodeToString(sum[:]), nil
	case "sha512":
		sum := sha512.Sum512([]byte(input))
		return hex.EncodeToString(sum[:]), nil
	case "b64encode":
		return base64.StdEncoding.EncodeToString([]byte(input)), nil
	case "b64decode":
		decoded, err := base64.StdEncoding.DecodeString(input)
		if err != nil {
			return "", fmt.Errorf("invalid base64: %w", err)
		}
		return string(decoded), nil
	case "urlencode":
		return url.QueryEscape(input), nil
	case "urldecode":
		decoded, err := url.QueryUnescape(input)
		if err != nil {
			return "", fmt.Errorf("invalid URL encoding: %w", err)
		}
		return decoded, nil
	default:
		return "", fmt.Errorf("unsupported operation")
	}
}

package main

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"net/http"
	"strconv"

	"github.com/lijiahao818/airlock/internal/advisor"
	"github.com/lijiahao818/airlock/internal/cabin"
	"github.com/lijiahao818/airlock/internal/console"
	"github.com/lijiahao818/airlock/internal/desktop"
	"github.com/lijiahao818/airlock/internal/filecheck"
	"github.com/lijiahao818/airlock/internal/gate"
	"github.com/lijiahao818/airlock/internal/sandbox"
	"github.com/lijiahao818/airlock/internal/vault"
	"github.com/lijiahao818/airlock/internal/webterm"
)

// app 是组装好的程序。控制台和网页终端各占一个端口，所以两者是不同的源（docs/architecture.md 第 5.11 节）。
type app struct {
	consoleLn, termLn net.Listener
	console, term     http.Handler
	advisorGate       *gate.Gate
	token             string // 控制台令牌，只出现在打开浏览器的网址片段中
}

// assemble 组装全部模块。secret 是真实密钥的来源，正式运行时为 vault.Get。
func assemble(dataDir string, backend sandbox.Backend, secret func(provider string) (string, error)) (a *app, err error) {
	a = &app{}
	defer func() {
		if err != nil {
			a.close()
		}
	}()
	if a.consoleLn, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
		return nil, err
	}
	if a.termLn, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
		return nil, err
	}
	termURL := "http://" + a.termLn.Addr().String()

	if a.advisorGate, err = startAdvisorGate(secret); err != nil {
		return nil, err
	}
	adv, err := advisor.New(advisor.Config{
		DataDir:     dataDir,
		BaseURL:     "http://127.0.0.1:" + strconv.Itoa(a.advisorGate.Port()),
		Placeholder: a.advisorGate.Placeholder,
		HasKey:      func(p string) bool { _, err := secret(p); return err == nil },
	})
	if err != nil {
		return nil, err
	}
	cabins, err := cabin.New(cabin.Options{
		DataDir:   dataDir,
		Backend:   backend,
		Secret:    secret,
		TermURL:   termURL,
		Explainer: adv,
		Inspector: filecheck.Checker{},
	})
	if err != nil {
		return nil, err
	}

	a.token = rand.Text()
	a.console = console.New(console.Deps{Cabins: cabins, Keys: keys{}, Advisor: adv, PickFolder: desktop.PickFolder}, a.token)
	a.term = webterm.New(cabins, termURL)
	return a, nil
}

// url 是用浏览器打开控制台的网址。令牌在片段中，不发给服务器（第 5.10 节）。
func (a *app) url() string { return "http://" + a.consoleLn.Addr().String() + "/#token=" + a.token }

func (a *app) close() {
	for _, ln := range []net.Listener{a.consoleLn, a.termLn} {
		if ln != nil {
			ln.Close()
		}
	}
	if a.advisorGate != nil {
		a.advisorGate.Close()
	}
}

// keys 用 vault 和 gate.TestKey 实现 console.Keys。
type keys struct{}

var _ console.Keys = keys{}

func (keys) Hint(provider string) (string, error) { return vault.Hint(provider) }
func (keys) Set(provider, secret string) error    { return vault.Set(provider, secret) }
func (keys) Delete(provider string) error         { return vault.Delete(provider) }

func (keys) Test(ctx context.Context, provider string) error {
	secret, err := vault.Get(provider)
	if errors.Is(err, vault.ErrNotFound) {
		return errors.New("还没有保存这家供应商的密钥。请先添加密钥，再点测试")
	}
	if err != nil {
		return err
	}
	return gate.TestKey(ctx, provider, secret)
}

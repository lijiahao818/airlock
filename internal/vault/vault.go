package vault

import "errors"

var (
	ErrNotFound       = errors.New("vault: 没有找到密钥")
	errNotImplemented = errors.New("vault: 未实现")
)

func Set(provider, secret string) error { return errNotImplemented }

func Get(provider string) (string, error) { return "", errNotImplemented }

func Delete(provider string) error { return errNotImplemented }

// Hint 返回密钥的后 4 位。
func Hint(provider string) (string, error) { return "", errNotImplemented }

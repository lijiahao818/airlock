package vault

import "errors"

var (
	ErrNotFound    = errors.New("vault: 没有找到密钥")
	errBadProvider = errors.New("vault: 供应商名称只能含小写字母、数字和连字符")
	errEmpty       = errors.New("vault: 密钥不能为空")
)

// Set 把密钥存入系统钥匙串。条目名为 airlock:<provider>。
func Set(provider, secret string) error {
	if err := check(provider); err != nil {
		return err
	}
	if secret == "" {
		return errEmpty
	}
	return write(target(provider), []byte(secret))
}

func Get(provider string) (string, error) {
	if err := check(provider); err != nil {
		return "", err
	}
	b, err := read(target(provider))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func Delete(provider string) error {
	if err := check(provider); err != nil {
		return err
	}
	return remove(target(provider))
}

// Hint 返回密钥的后 4 位。
func Hint(provider string) (string, error) {
	s, err := Get(provider)
	if err != nil {
		return "", err
	}
	return hint(s), nil
}

func hint(secret string) string {
	r := []rune(secret)
	return string(r[max(0, len(r)-4):])
}

func target(provider string) string { return "airlock:" + provider }

// check 在调用钥匙串前拒绝不合法的供应商名称，名称会拼进条目名。
func check(provider string) error {
	if provider == "" || len(provider) > 64 {
		return errBadProvider
	}
	for _, c := range provider {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return errBadProvider
		}
	}
	return nil
}

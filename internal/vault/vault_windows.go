package vault

import (
	"errors"
	"syscall"
	"unsafe"
)

// 凭据管理器的接口见 https://learn.microsoft.com/en-us/windows/win32/api/wincred/。
var (
	advapi32   = syscall.NewLazyDLL("advapi32.dll")
	credWrite  = advapi32.NewProc("CredWriteW")
	credRead   = advapi32.NewProc("CredReadW")
	credDelete = advapi32.NewProc("CredDeleteW")
	credFree   = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	errorNotFound           = syscall.Errno(1168)
)

// credential 对应 wincred.h 中的 CREDENTIALW，字段顺序和类型必须一致。
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

func write(target string, secret []byte) error {
	name, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	c := credential{
		Type:               credTypeGeneric,
		TargetName:         name,
		CredentialBlobSize: uint32(len(secret)),
		CredentialBlob:     unsafe.SliceData(secret),
		Persist:            credPersistLocalMachine,
	}
	if ok, _, err := credWrite.Call(uintptr(unsafe.Pointer(&c)), 0); ok == 0 {
		return wrap("保存", err)
	}
	return nil
}

func read(target string) ([]byte, error) {
	name, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return nil, err
	}
	var c *credential
	if ok, _, err := credRead.Call(uintptr(unsafe.Pointer(name)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&c))); ok == 0 {
		return nil, wrap("读取", err)
	}
	defer credFree.Call(uintptr(unsafe.Pointer(c)))
	// 复制一份，CredFree 之后原来的内存失效。
	return append([]byte(nil), unsafe.Slice(c.CredentialBlob, c.CredentialBlobSize)...), nil
}

func remove(target string) error {
	name, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	if ok, _, err := credDelete.Call(uintptr(unsafe.Pointer(name)), credTypeGeneric, 0); ok == 0 {
		return wrap("删除", err)
	}
	return nil
}

// wrap 把系统错误转为 vault 的错误。错误中只有操作名和错误码，不含密钥。
func wrap(op string, err error) error {
	if errors.Is(err, errorNotFound) {
		return ErrNotFound
	}
	return errors.New("vault: " + op + "密钥失败：" + err.Error())
}

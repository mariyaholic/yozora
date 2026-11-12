//go:build windows

package update

import (
	"syscall"
	"unsafe"
)

var (
	modWintrust  = syscall.NewLazyDLL("wintrust.dll")
	modCrypt32   = syscall.NewLazyDLL("crypt32.dll")
	modKernel32  = syscall.NewLazyDLL("kernel32.dll")
	procVerify   = modWintrust.NewProc("WinVerifyTrust")
	procProvData = modWintrust.NewProc("WTHelperProvDataFromStateData")
	procProvSgnr = modWintrust.NewProc("WTHelperGetProvSignerFromChain")
	procProvCert = modWintrust.NewProc("WTHelperGetProvCertFromChain")
	procNameStr  = modCrypt32.NewProc("CertGetNameStringW")
	procMove     = modKernel32.NewProc("RtlMoveMemory")
)

const (
	wtdUINone           = 2
	wtdRevokeNone       = 0
	wtdChoiceFile       = 1
	wtdStateVerify      = 1
	wtdStateClose       = 2
	wtdCacheOnlyURL     = 0x1000 // never reach the network while checking
	certNameRDNType     = 2
	certX500NameStr     = 3
	subjectBufferUint16 = 512
)

// WINTRUST_ACTION_GENERIC_VERIFY_V2
var actionGenericVerifyV2 = [16]byte{0x6b, 0xc5, 0x0a, 0x00, 0x44, 0xcd, 0xd0, 0x11, 0x8c, 0xc2, 0x00, 0xc0, 0x4f, 0xc2, 0x95, 0xee}

type wintrustFileInfo struct {
	cbStruct     uint32
	filePath     *uint16
	file         uintptr
	knownSubject uintptr
}

type wintrustData struct {
	cbStruct          uint32
	policyCallback    uintptr
	sipClient         uintptr
	uiChoice          uint32
	revocationChecks  uint32
	unionChoice       uint32
	file              *wintrustFileInfo
	stateAction       uint32
	stateData         uintptr
	urlReference      *uint16
	provFlags         uint32
	uiContext         uint32
	signatureSettings uintptr
}

// FileSignature checks the Authenticode signature of an exe and names its
// signer. Revocation is not checked and the network is not used, so a check
// never stalls; the comparison that matters is subject against subject.
func FileSignature(path string) Signature {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return Signature{}
	}
	file := wintrustFileInfo{filePath: p}
	file.cbStruct = uint32(unsafe.Sizeof(file))
	data := wintrustData{
		uiChoice: wtdUINone, revocationChecks: wtdRevokeNone, unionChoice: wtdChoiceFile,
		file: &file, stateAction: wtdStateVerify, provFlags: wtdCacheOnlyURL,
	}
	data.cbStruct = uint32(unsafe.Sizeof(data))
	r, _, _ := procVerify.Call(^uintptr(0), uintptr(unsafe.Pointer(&actionGenericVerifyV2)), uintptr(unsafe.Pointer(&data)))
	defer func() {
		data.stateAction = wtdStateClose
		procVerify.Call(^uintptr(0), uintptr(unsafe.Pointer(&actionGenericVerifyV2)), uintptr(unsafe.Pointer(&data)))
	}()
	if int32(r) != 0 {
		return Signature{}
	}
	// A signature whose signer cannot be named is no use for the same-publisher
	// comparison, so it counts as unsigned. (Wine's WinVerifyTrust also answers
	// success for anything, which this keeps out of the way.)
	subject := signerSubject(data.stateData)
	if subject == "" {
		return Signature{}
	}
	return Signature{Valid: true, Subject: subject}
}

// signerSubject reads the leaf certificate's subject out of the verification
// state. The helpers return pointers into wintrust's own memory, which are
// only ever passed back or copied out with RtlMoveMemory, never dereferenced here.
func signerSubject(state uintptr) string {
	if state == 0 {
		return ""
	}
	prov, _, _ := procProvData.Call(state)
	if prov == 0 {
		return ""
	}
	sgnr, _, _ := procProvSgnr.Call(prov, 0, 0, 0)
	if sgnr == 0 {
		return ""
	}
	cert, _, _ := procProvCert.Call(sgnr, 0)
	if cert == 0 {
		return ""
	}
	// CRYPT_PROVIDER_CERT { DWORD cbStruct; PCCERT_CONTEXT pCert; ... }: pCert is at offset 8.
	var ctx uintptr
	procMove.Call(uintptr(unsafe.Pointer(&ctx)), cert+unsafe.Sizeof(uintptr(0)), unsafe.Sizeof(ctx))
	if ctx == 0 {
		return ""
	}
	var buf [subjectBufferUint16]uint16
	strType := uint32(certX500NameStr)
	n, _, _ := procNameStr.Call(ctx, certNameRDNType, 0, uintptr(unsafe.Pointer(&strType)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n <= 1 {
		return ""
	}
	return syscall.UTF16ToString(buf[:])
}

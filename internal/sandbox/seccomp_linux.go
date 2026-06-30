//go:build linux

package sandbox

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// BPF instruction classes and modifiers for building seccomp filters.
const (
	bpfLD  = 0x00
	bpfW   = 0x00
	bpfABS = 0x20
	bpfJMP = 0x05
	bpfJEQ = 0x10
	bpfRET = 0x06
	bpfK   = 0x00

	seccompRetAllow = 0x7fff0000
	seccompRetErrno = 0x00050000 // SECCOMP_RET_ERRNO
	ePERM           = 1

	// Offset of the syscall number in struct seccomp_data.
	seccompDataNr = 0
)

// sockFilter is the BPF instruction structure (struct sock_filter).
type sockFilter struct {
	Code uint16 // filter code
	Jt   uint8  // true jump displacement
	Jf   uint8  // false jump displacement
	K    uint32 // generic multiuse field
}

// blockedSyscallNums returns the syscall numbers for dangerous system calls
// that should be blocked in all sandbox modes. The numbers are architecture-
// specific; we use numeric constants to avoid compilation failures on
// architectures where certain syscall.SYS_* constants don't exist.
func blockedSyscallNums() []uintptr {
	switch runtime.GOARCH {
	case "amd64":
		// Source: arch/x86/entry/syscalls/syscall_64.tbl
		return []uintptr{
			165, 166, 101, 246, 264, // mount, umount2, ptrace, kexec_load, open_by_handle_at
			175, 313, 176, 110, 173, // init_module, finit_module, delete_module, iopl, ioperm
			167, 168, 103, 298, 300, // swapon, swapoff, syslog, perf_event_open, fanotify_init
			321, 250, 306, 212,       // bpf, keyctl, kexec_file_load, lookup_dcookie
			310, 311,                 // process_vm_readv, process_vm_writev
		}
	case "arm64":
		// Source: include/uapi/asm-generic/unistd.h (arm64 uses asm-generic)
		return []uintptr{
			40, 39, 117, 104, 64,    // mount, umount2, ptrace, kexec_load, open_by_handle_at
			105, 219, 106,           // init_module, finit_module, delete_module (no iopl/ioperm)
			224, 225, 116, 241, 262, // swapon, swapoff, syslog, perf_event_open, fanotify_init
			280, 250, 294, 18,       // bpf, keyctl, kexec_file_load, lookup_dcookie
			270, 271,                // process_vm_readv, process_vm_writev
		}
	case "386":
		// Source: arch/x86/entry/syscalls/syscall_32.tbl
		return []uintptr{
			21, 52, 26, 248, 335,    // mount, umount2, ptrace, kexec_load, open_by_handle_at
			128, 350, 129, 110, 101, // init_module, finit_module, delete_module, iopl, ioperm
			87, 115, 103, 336, 338,  // swapon, swapoff, syslog, perf_event_open, fanotify_init
			357, 288, 320, 253,      // bpf, keyctl, kexec_file_load, lookup_dcookie
			347, 348,                // process_vm_readv, process_vm_writev
		}
	default:
		// Unsupported architecture: rely on bwrap filesystem isolation alone.
		return nil
	}
}

// writeBlocklistNums returns the syscall numbers for filesystem-modifying
// system calls that are additionally blocked in read-only sandbox mode.
func writeBlocklistNums() []uintptr {
	switch runtime.GOARCH {
	case "amd64":
		return []uintptr{
			2, 86, 87, 82, 90,   // creat, link, unlink, rename, chmod
			91, 92, 93, 76, 77,  // fchmod, chown, fchown, truncate, ftruncate
			133, 88, 83, 84,     // mknod, symlink, mkdir, rmdir
		}
	case "arm64":
		// arm64 doesn't have direct creat/link/unlink/rename/chmod/chown/
		// symlink/mkdir/rmdir syscalls — they use the *at variants.
		// Block the ones that do exist.
		return []uintptr{
			45, 46,           // truncate, ftruncate
			52, 55,           // fchmod, fchown
			33,               // mknodat (only mknod variant on arm64)
		}
	case "386":
		return []uintptr{
			8, 9, 10, 38, 15,  // creat, link, unlink, rename, chmod
			16, 17, 18, 92, 93, // fchmod, chown, fchown, truncate, ftruncate
			14, 83, 39, 40,    // mknod, symlink, mkdir, rmdir
		}
	default:
		return nil
	}
}

// generateSeccompBPF builds a BPF program that:
//  1. Loads the syscall number from seccomp_data.nr.
//  2. Checks it against the universal blocklist (dangerous syscalls).
//  3. If readOnly, also checks against the write-mode blocklist.
//  4. Returns SECCOMP_RET_ERRNO(EPERM) for blocked calls,
//     SECCOMP_RET_ALLOW for everything else.
func generateSeccompBPF(readOnly bool) []sockFilter {
	blocked := blockedSyscallNums()
	if readOnly {
		blocked = append(blocked, writeBlocklistNums()...)
	}

	if len(blocked) == 0 {
		// Unsupported arch: return trivial allow-all (bwrap still provides
		// filesystem isolation).
		return []sockFilter{
			{Code: bpfRET | bpfK, K: seccompRetAllow},
		}
	}

	n := len(blocked)
	// Program layout:
	//   [0]     load syscall number
	//   [1..n]  compare against each blocked syscall (jump to RET_ERRNO if match)
	//   [n+1]   RET_ALLOW
	//   [n+2]   RET_ERRNO
	prog := make([]sockFilter, 0, n+3)

	// Instruction 0: load syscall number
	prog = append(prog, sockFilter{
		Code: bpfLD | bpfW | bpfABS,
		K:    seccompDataNr,
	})

	// Instructions 1..n: compare against each blocked syscall.
	for i, sysno := range blocked {
		jumpToErrno := uint8(n - i + 1)
		prog = append(prog, sockFilter{
			Code: bpfJMP | bpfJEQ | bpfK,
			Jt:   jumpToErrno,
			K:    uint32(sysno),
		})
	}

	// RET_ALLOW
	prog = append(prog, sockFilter{
		Code: bpfRET | bpfK,
		K:    seccompRetAllow,
	})

	// RET_ERRNO(EPERM)
	prog = append(prog, sockFilter{
		Code: bpfRET | bpfK,
		K:    seccompRetErrno | ePERM,
	})

	return prog
}

// marshalBPF serializes a slice of sockFilter into the binary format expected
// by bwrap --seccomp: a 16-bit instruction count followed by the instructions
// themselves, each 8 bytes (code:2 + jt:1 + jf:1 + k:4, host byte order).
func marshalBPF(filters []sockFilter) ([]byte, error) {
	if len(filters) > 65535 {
		return nil, fmt.Errorf("seccomp: BPF program too large (%d instructions)", len(filters))
	}
	buf := make([]byte, 2+8*len(filters))
	bo := binary.NativeEndian
	bo.PutUint16(buf[0:2], uint16(len(filters)))
	for i, f := range filters {
		off := 2 + 8*i
		bo.PutUint16(buf[off:off+2], f.Code)
		buf[off+2] = f.Jt
		buf[off+3] = f.Jf
		bo.PutUint32(buf[off+4:off+8], f.K)
	}
	return buf, nil
}

// createSeccompBPFFile creates and opens a seccomp BPF temp file, returning
// the *os.File (for passing as ExtraFiles to exec.Cmd). The temp file is
// unlinked from the filesystem so it is cleaned up when the fd is closed.
func createSeccompBPFFile(readOnly bool) (*os.File, error) {
	filters := generateSeccompBPF(readOnly)
	data, err := marshalBPF(filters)
	if err != nil {
		return nil, fmt.Errorf("seccomp: marshal BPF: %w", err)
	}

	f, err := os.CreateTemp("", "seccomp-bpf-*.bin")
	if err != nil {
		return nil, fmt.Errorf("seccomp: create temp file: %w", err)
	}
	os.Remove(f.Name()) // unlink; fd remains valid

	if _, err := f.Write(data); err != nil {
		f.Close()
		return nil, fmt.Errorf("seccomp: write BPF: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("seccomp: seek: %w", err)
	}
	return f, nil
}

// linuxSandbox is the Linux CmdSandbox implementation. It carries the
// seccomp BPF file descriptor for bwrap's --seccomp argument.
type linuxSandbox struct {
	noopSandbox
	fd int
}

// SeccompFD returns the file descriptor number for bwrap's --seccomp argument.
func (s *linuxSandbox) SeccompFD() int { return s.fd }

// configureCmdOS is the Linux implementation of ConfigureCmd.
// It attaches the seccomp BPF filter file as an ExtraFile on cmd and returns
// a CmdSandbox whose SeccompFD field holds the file descriptor number that
// bwrap's --seccomp argument should reference. When spec.Seccomp is false,
// SeccompFD is 0.
func configureCmdOS(spec Spec, cmd *exec.Cmd) (CmdSandbox, error) {
	if !spec.Seccomp {
		return noopSandbox{}, nil
	}

	readOnly := spec.EffectiveMode() == SandboxReadOnly
	bpfFile, err := createSeccompBPFFile(readOnly)
	if err != nil {
		// Seccomp is best-effort: skip rather than fail.
		return noopSandbox{}, nil
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, bpfFile)
	// fd 3 is the first ExtraFile (0=stdin, 1=stdout, 2=stderr, 3=first extra).
	fd := 3 + len(cmd.ExtraFiles) - 1
	return &linuxSandbox{fd: fd}, nil
}

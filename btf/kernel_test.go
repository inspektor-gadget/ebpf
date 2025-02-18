package btf

import (
	"os"
	"testing"

	"github.com/go-quicktest/qt"
)

func TestLoadKernelSpec(t *testing.T) {
	if _, err := os.Stat("/sys/kernel/btf/vmlinux"); os.IsNotExist(err) {
		t.Skip("/sys/kernel/btf/vmlinux not present")
	}

	_, err := LoadKernelSpec()
	if err != nil {
		t.Fatal("Can't load kernel spec:", err)
	}
}

func TestLoadKernelModuleSpec(t *testing.T) {
	if _, err := os.Stat("/sys/kernel/btf/btf_testmod"); os.IsNotExist(err) {
		t.Skip("/sys/kernel/btf/btf_testmod not present")
	}

	_, err := LoadKernelModuleSpec("btf_testmod")
	qt.Assert(t, qt.IsNil(err))
}

func TestLoadKernelSpecWithOpts(t *testing.T) {
	if _, err := os.Stat("/sys/kernel/btf/vmlinux"); os.IsNotExist(err) {
		t.Skip("/sys/kernel/btf/vmlinux not present")
	}

	opts := &SpecOptions{
		TypeNames: map[string]struct{}{
			"task_struct": {},
			"pt_regs":     {},
			"socket":      {},
		},
	}

	spec1, err := LoadKernelSpecWithOptions(opts)
	if err != nil {
		t.Fatal("Can't load kernel spec:", err)
	}

	spec2, err := LoadKernelSpecWithOptions(opts)
	if err != nil {
		t.Fatal("Can't load kernel spec:", err)
	}

	qt.Assert(t, qt.Equals(len(spec1.imm.types), len(spec2.imm.types)))
	qt.Assert(t, qt.Equals(len(spec1.imm.typeIDs), len(spec2.imm.typeIDs)))
	qt.Assert(t, qt.DeepEquals(spec1.imm.namedTypes, spec2.imm.namedTypes))
}

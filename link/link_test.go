package link

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/internal/sys"
	"github.com/cilium/ebpf/internal/testutils"
	"github.com/cilium/ebpf/internal/testutils/testmain"
	"github.com/cilium/ebpf/internal/unix"

	"github.com/go-quicktest/qt"
)

func TestMain(m *testing.M) {
	testmain.Run(m)
}

func TestRawLink(t *testing.T) {
	cgroup, prog := mustCgroupFixtures(t)

	link, err := AttachRawLink(RawLinkOptions{
		Target:  int(cgroup.Fd()),
		Program: prog,
		Attach:  ebpf.AttachCGroupInetEgress,
	})
	testutils.SkipIfNotSupported(t, err)
	if err != nil {
		t.Fatal("Can't create raw link:", err)
	}

	info, err := link.Info()
	if err != nil {
		t.Fatal("Can't get link info:", err)
	}

	pi, err := prog.Info()
	if err != nil {
		t.Fatal("Can't get program info:", err)
	}

	progID, ok := pi.ID()
	if !ok {
		t.Fatal("Program ID not available in program info")
	}

	if info.Program != progID {
		t.Error("Link program ID doesn't match program ID")
	}

	testLink(t, link, prog)
}

func TestUnpinRawLink(t *testing.T) {
	cgroup, prog := mustCgroupFixtures(t)
	link, _ := newPinnedRawLink(t, cgroup, prog)
	defer link.Close()

	qt.Assert(t, qt.IsTrue(link.IsPinned()))

	if err := link.Unpin(); err != nil {
		t.Fatal(err)
	}

	qt.Assert(t, qt.IsFalse(link.IsPinned()))
}

func TestRawLinkLoadPinnedWithOptions(t *testing.T) {
	cgroup, prog := mustCgroupFixtures(t)
	link, path := newPinnedRawLink(t, cgroup, prog)
	defer link.Close()

	qt.Assert(t, qt.IsTrue(link.IsPinned()))

	// It seems like the kernel ignores BPF_F_RDONLY when updating a link,
	// so we can't test this.
	_, err := loadPinnedRawLink(path, &ebpf.LoadPinOptions{
		Flags: math.MaxUint32,
	})
	if !errors.Is(err, unix.EINVAL) {
		t.Fatal("Invalid flags don't trigger an error:", err)
	}
}

func TestIterator(t *testing.T) {
	cgroup, prog := mustCgroupFixtures(t)

	tLink, err := AttachRawLink(RawLinkOptions{
		Target:  int(cgroup.Fd()),
		Program: prog,
		Attach:  ebpf.AttachCGroupInetEgress,
	})
	testutils.SkipIfNotSupported(t, err)
	if err != nil {
		t.Fatal("Can't create original raw link:", err)
	}
	defer tLink.Close()
	tLinkInfo, err := tLink.Info()
	testutils.SkipIfNotSupported(t, err)
	if err != nil {
		t.Fatal("Can't get original link info:", err)
	}

	it := new(Iterator)
	defer it.Close()

	prev := it.ID
	var foundLink Link
	for it.Next() {
		// Iterate all loaded links.
		if it.Link == nil {
			t.Fatal("Next doesn't assign link")
		}
		if it.ID == prev {
			t.Fatal("Iterator doesn't advance ID")
		}
		prev = it.ID
		if it.ID == tLinkInfo.ID {
			foundLink = it.Take()
		}
	}
	if err := it.Err(); err != nil {
		t.Fatal("Iteration returned an error:", err)
	}
	if it.Link != nil {
		t.Fatal("Next doesn't clean up link on last iteration")
	}
	if prev != it.ID {
		t.Fatal("Next changes ID on last iteration")
	}
	if foundLink == nil {
		t.Fatal("Original link not found")
	}
	defer foundLink.Close()
	// Confirm that we found the original link.
	info, err := foundLink.Info()
	if err != nil {
		t.Fatal("Can't get link info:", err)
	}
	if info.ID != tLinkInfo.ID {
		t.Fatal("Found link has wrong ID")
	}

}

func newPinnedRawLink(t *testing.T, cgroup *os.File, prog *ebpf.Program) (*RawLink, string) {
	t.Helper()

	link, err := AttachRawLink(RawLinkOptions{
		Target:  int(cgroup.Fd()),
		Program: prog,
		Attach:  ebpf.AttachCGroupInetEgress,
	})
	testutils.SkipIfNotSupported(t, err)
	if err != nil {
		t.Fatal("Can't create raw link:", err)
	}

	path := filepath.Join(testutils.TempBPFFS(t), "link")
	err = link.Pin(path)
	testutils.SkipIfNotSupported(t, err)
	if err != nil {
		t.Fatal(err)
	}

	return link, path
}

func mustCgroupFixtures(t *testing.T) (*os.File, *ebpf.Program) {
	t.Helper()

	testutils.SkipIfNotSupported(t, haveProgAttach())

	return testutils.CreateCgroup(t), mustLoadProgram(t, ebpf.CGroupSKB, 0, "")
}

func testLink(t *testing.T, link Link, prog *ebpf.Program) {
	t.Helper()

	tmp, err := os.MkdirTemp("/sys/fs/bpf", "ebpf-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	_, isRawLink := link.(*RawLink)

	t.Run("link/pinning", func(t *testing.T) {
		path := filepath.Join(tmp, "link")
		err = link.Pin(path)
		testutils.SkipIfNotSupported(t, err)
		if err != nil {
			t.Fatalf("Can't pin %T: %s", link, err)
		}

		link2, err := LoadPinnedLink(path, nil)
		if err != nil {
			t.Fatalf("Can't load pinned %T: %s", link, err)
		}
		link2.Close()

		if !isRawLink && reflect.TypeOf(link) != reflect.TypeOf(link2) {
			t.Errorf("Loading a pinned %T returns a %T", link, link2)
		}

		_, err = LoadPinnedLink(path, &ebpf.LoadPinOptions{
			Flags: math.MaxUint32,
		})
		if !errors.Is(err, unix.EINVAL) {
			t.Errorf("Loading a pinned %T doesn't respect flags", link)
		}
	})

	t.Run("link/update", func(t *testing.T) {
		err := link.Update(prog)
		testutils.SkipIfNotSupported(t, err)
		if err != nil {
			t.Fatal("Update returns an error:", err)
		}

		func() {
			// Panicking is OK
			defer func() {
				_ = recover()
			}()

			if err := link.Update(nil); err == nil {
				t.Fatalf("%T.Update accepts nil program", link)
			}
		}()
	})

	t.Run("link/info", func(t *testing.T) {
		info, err := link.Info()
		testutils.SkipIfNotSupported(t, err)
		if err != nil {
			t.Fatal("Link info returns an error:", err)
		}

		if info.Type == 0 {
			t.Fatal("Failed to get link info type")
		}

		switch link.(type) {
		case *tracing:
			if info.Tracing() == nil {
				t.Fatalf("Failed to get link tracing extra info")
			}
		case *linkCgroup:
			cg := info.Cgroup()
			if cg.CgroupId == 0 {
				t.Fatalf("Failed to get link Cgroup extra info")
			}
		case *NetNsLink:
			netns := info.NetNs()
			if netns.AttachType == 0 {
				t.Fatalf("Failed to get link NetNs extra info")
			}
		case *xdpLink:
			xdp := info.XDP()
			if xdp.Ifindex == 0 {
				t.Fatalf("Failed to get link XDP extra info")
			}
		case *tcxLink:
			tcx := info.TCX()
			if tcx.Ifindex == 0 {
				t.Fatalf("Failed to get link TCX extra info")
			}
		case *netfilterLink:
			nf := info.Netfilter()
			if nf.Priority == 0 {
				t.Fatalf("Failed to get link Netfilter extra info")
			}
		case *kprobeMultiLink:
			// test default Info data
			kmulti := info.KprobeMulti()
			if count, ok := kmulti.AddressCount(); ok {
				qt.Assert(t, qt.Not(qt.Equals(count, 0)))

				_, ok = kmulti.Missed()
				qt.Assert(t, qt.IsTrue(ok))
				// NB: We don't check that missed is actually correct
				// since it's not easy to trigger from tests.
			}
		case *perfEventLink:
			// test default Info data
			pevent := info.PerfEvent()
			switch pevent.Type {
			case sys.BPF_PERF_EVENT_KPROBE, sys.BPF_PERF_EVENT_KRETPROBE:
				kp := pevent.Kprobe()
				if addr, ok := kp.Address(); ok {
					qt.Assert(t, qt.Not(qt.Equals(addr, 0)))

					_, ok := kp.Missed()
					qt.Assert(t, qt.IsTrue(ok))
					// NB: We don't check that missed is actually correct
					// since it's not easy to trigger from tests.
				}
			}
		}
	})

	type FDer interface {
		FD() int
	}

	t.Run("from fd", func(t *testing.T) {
		fder, ok := link.(FDer)
		if !ok {
			t.Skip("Link doesn't allow retrieving FD")
		}

		// We need to dup the FD since NewLinkFromFD takes
		// ownership.
		dupFD, err := unix.FcntlInt(uintptr(fder.FD()), unix.F_DUPFD_CLOEXEC, 1)
		if err != nil {
			t.Fatal("Can't dup link FD:", err)
		}
		defer unix.Close(dupFD)

		newLink, err := NewFromFD(dupFD)
		testutils.SkipIfNotSupported(t, err)
		if err != nil {
			t.Fatal("Can't create new link from dup link FD:", err)
		}
		defer newLink.Close()

		if !isRawLink && reflect.TypeOf(newLink) != reflect.TypeOf(link) {
			t.Fatalf("Expected type %T, got %T", link, newLink)
		}
	})

	if err := link.Close(); err != nil {
		t.Fatalf("%T.Close returns an error: %s", link, err)
	}
}

func mustLoadProgram(tb testing.TB, typ ebpf.ProgramType, attachType ebpf.AttachType, attachTo string) *ebpf.Program {
	tb.Helper()

	license := "MIT"
	switch typ {
	case ebpf.RawTracepoint, ebpf.LSM:
		license = "GPL"
	}

	prog, err := ebpf.NewProgram(&ebpf.ProgramSpec{
		Type:       typ,
		AttachType: attachType,
		AttachTo:   attachTo,
		License:    license,
		Instructions: asm.Instructions{
			asm.Mov.Imm(asm.R0, 0),
			asm.Return(),
		},
	})
	if err != nil {
		tb.Fatal(err)
	}

	tb.Cleanup(func() {
		prog.Close()
	})

	return prog
}

func TestLoadWrongPin(t *testing.T) {
	cg, p := mustCgroupFixtures(t)

	l, err := AttachRawLink(RawLinkOptions{
		Target:  int(cg.Fd()),
		Program: p,
		Attach:  ebpf.AttachCGroupInetEgress,
	})
	testutils.SkipIfNotSupported(t, err)
	t.Cleanup(func() { l.Close() })

	tmp := testutils.TempBPFFS(t)

	ppath := filepath.Join(tmp, "prog")
	lpath := filepath.Join(tmp, "link")

	qt.Assert(t, qt.IsNil(p.Pin(ppath)))
	qt.Assert(t, qt.IsNil(l.Pin(lpath)))

	_, err = LoadPinnedLink(ppath, nil)
	qt.Assert(t, qt.IsNotNil(err))

	ll, err := LoadPinnedLink(lpath, nil)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(ll.Close()))
}

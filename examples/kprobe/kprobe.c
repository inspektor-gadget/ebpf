//go:build ignore

#include "common.h"

char __license[] SEC("license") = "Dual MIT/GPL";

struct bpf_map_def SEC("maps") kprobe_map = {
	.type        = BPF_MAP_TYPE_ARRAY,
	.key_size    = sizeof(u32),
	.value_size  = sizeof(u64),
	.max_entries = 1,
};

__attribute__((noinline)) __u64 sayhello(void) {
	bpf_printk("Hello, World!\n");
	return 0;
}

__attribute__((noinline)) __u64 sayfoo(void) {
	bpf_printk("Hello, Foo!\n");
	bpf_printk("Hello, Foo1!\n");
	bpf_printk("Hello, Foo2!\n");
	bpf_printk("Hello, Foo3!\n");
	return 0;
}

SEC("kprobe/sys_execve")
int kprobe_execve() {
	u32 key     = 0;
	u64 initval = 1, *valp;

	valp = bpf_map_lookup_elem(&kprobe_map, &key);
	if (!valp) {
		bpf_map_update_elem(&kprobe_map, &key, &initval, BPF_ANY);
		return 0;
	}
	__sync_fetch_and_add(valp, 1);

	sayhello();

	return 0;
}

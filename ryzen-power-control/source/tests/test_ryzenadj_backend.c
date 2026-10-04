/* Compile against the bundled RyzenAdj source; all hardware access is stubbed. */
#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include "lib/linux/osdep_linux_mem.h"
#include "lib/linux/osdep_linux_smu_kernel_module.h"

static int driver_present, mem_calls, smu_calls;
static os_access_obj_t mem_obj, smu_obj;
extern bool is_using_smu_driver(void);

int __wrap_lstat(const char *path, struct stat *st) { return driver_present ? 0 : -1; }
FILE *__wrap_fopen(const char *path, const char *mode) {
    if (!driver_present) return NULL;
    FILE *f = tmpfile(); assert(f);
    fputs("0.1.7\n", f); rewind(f); return f;
}
os_access_obj_t *init_os_access_obj_mem(void) { ++mem_calls; return &mem_obj; }
os_access_obj_t *init_os_access_obj_kmod(void) { ++smu_calls; return &smu_obj; }
int init_mem_obj_mem(os_access_obj_t *o, uintptr_t a) { return 0; }
int init_mem_obj_kmod(os_access_obj_t *o, uintptr_t a) { return 0; }
void free_os_access_obj_mem(os_access_obj_t *o) {}
void free_os_access_obj_kmod(os_access_obj_t *o) {}
uint32_t smn_reg_read_mem(const os_access_obj_t *o, uint32_t a) { return 0; }
uint32_t smn_reg_read_kmod(const os_access_obj_t *o, uint32_t a) { return 0; }
void smn_reg_write_mem(const os_access_obj_t *o, uint32_t a, uint32_t d) {}
void smn_reg_write_kmod(const os_access_obj_t *o, uint32_t a, uint32_t d) {}
int copy_pm_table_mem(const os_access_obj_t *o, void *b, size_t s) { return 0; }
int copy_pm_table_kmod(const os_access_obj_t *o, void *b, size_t s) { return 0; }
int compare_pm_table_mem(const void *b, size_t s) { return 0; }
int compare_pm_table_kmod(const void *b, size_t s) { return 0; }

int main(void) {
    setenv("RYZENADJ_BACKEND", "smu", 1);
    assert(init_os_access_obj() == NULL);
    assert(mem_calls == 0 && smu_calls == 0);
    driver_present = 1;
    assert(init_os_access_obj() == &smu_obj && is_using_smu_driver());
    setenv("RYZENADJ_BACKEND", "mem", 1);
    assert(init_os_access_obj() == &mem_obj && !is_using_smu_driver());
    setenv("RYZENADJ_BACKEND", "invalid", 1);
    assert(init_os_access_obj() == NULL);
    unsetenv("RYZENADJ_BACKEND");
    assert(init_os_access_obj() == &smu_obj);
    driver_present = 0;
    assert(init_os_access_obj() == &mem_obj);
    assert(mem_calls == 2 && smu_calls == 2);
    puts("backend selection checks passed (no hardware access)");
    return 0;
}

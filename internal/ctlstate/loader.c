//go:build linux && cgo

#include "loader.h"
#include <dlfcn.h>
#include <stdio.h>
#include <stdlib.h>

static void *library;
static int initialized;
static char load_error[1024];
static const char *missing;

void *ctlgo_lookup(const char *name)
{
    if (!initialized) {
        const char *path = getenv("LTTNG_CTL_GO_LIBRARY");
        if (!path || !*path) {
            path = "liblttng-ctl.so.0";
        }
        initialized = 1;
        library = dlopen(path, RTLD_NOW | RTLD_LOCAL);
        if (!library) {
            snprintf(load_error, sizeof(load_error), "%s", dlerror());
        }
        /* Never dlclose: native objects and their destructors may outlive calls. */
    }
    return library ? dlsym(library, name) : NULL;
}

void *ctlgo_require(const char *name)
{
    void *symbol = ctlgo_lookup(name);
    if (!symbol && !missing) {
        missing = name; /* Generated callers pass string literals. */
    }
    return symbol;
}

const char *ctlgo_load_error(void) { return load_error[0] ? load_error : NULL; }
const char *ctlgo_missing_symbol(void) { return missing; }
void ctlgo_reset_error(void) { missing = NULL; }

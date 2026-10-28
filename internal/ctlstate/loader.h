#ifndef CTLGO_LOADER_H
#define CTLGO_LOADER_H

/* All entry points require the shared Go library lock. */
void *ctlgo_lookup(const char *name);
void *ctlgo_require(const char *name);
const char *ctlgo_load_error(void);
const char *ctlgo_missing_symbol(void);
void ctlgo_reset_error(void);

#endif

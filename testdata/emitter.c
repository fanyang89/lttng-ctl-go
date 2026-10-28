/* Emit only after the Go test has installed a recording rule and started it. */
#include <stdio.h>
#include <lttng/tracef.h>

int main(void)
{
    puts("ready");
    fflush(stdout);
    if (getchar() == EOF) {
        return 1;
    }
    tracef("lttng-ctl-go accepted");
    tracef("lttng-ctl-go rejected");
    return 0;
}

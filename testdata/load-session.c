/* Native reference for comparing Go load results with unwrapped liblttng-ctl. */
#include <lttng/load.h>
#include <stdio.h>

int main(int argc, char **argv)
{
    if (argc != 3) {
        return 2;
    }
    struct lttng_load_session_attr *attr = lttng_load_session_attr_create();
    if (!attr) {
        return 3;
    }
    int result = lttng_load_session_attr_set_input_url(attr, argv[1]);
    if (!result) {
        result = lttng_load_session_attr_set_session_name(attr, argv[2]);
    }
    if (!result) {
        result = lttng_load_session(attr);
    }
    printf("%d\n", result);
    lttng_load_session_attr_destroy(attr);
    return 0;
}

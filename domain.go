//go:build linux && cgo

package lttng

/*
#include <stdlib.h>
#include "internal/ctlstate/bridge_generated.h"
*/
import "C"

import (
	"context"
	"unsafe"
)

// DomainType selects a tracer. This SDK currently supports UST and kernel tracing.
type DomainType int

const (
	DomainKernel DomainType = C.LTTNG_DOMAIN_KERNEL
	DomainUST    DomainType = C.LTTNG_DOMAIN_UST
)

// BufferType selects ownership of channel buffers.
type BufferType int

const (
	BufferPerPID BufferType = C.LTTNG_BUFFER_PER_PID
	BufferPerUID BufferType = C.LTTNG_BUFFER_PER_UID
	BufferGlobal BufferType = C.LTTNG_BUFFER_GLOBAL
)

// Domain identifies a tracer and its buffer ownership model. All channels of a
// session in the same domain must use the same BufferType. Kernel tracing
// requires BufferGlobal. UST supports BufferPerPID (zero) and BufferPerUID.
type Domain struct {
	Type       DomainType
	BufferType BufferType
}

func (domain Domain) validate(op string) error {
	switch domain.Type {
	case DomainUST:
		if domain.BufferType == BufferPerPID || domain.BufferType == BufferPerUID {
			return nil
		}
	case DomainKernel:
		if domain.BufferType == BufferGlobal {
			return nil
		}
	}
	return invalid(op, "unsupported domain or buffer ownership model")
}

func (domain Domain) native() C.struct_lttng_domain {
	return C.struct_lttng_domain{
		_type:    C.enum_lttng_domain_type(domain.Type),
		buf_type: C.enum_lttng_buffer_type(domain.BufferType),
	}
}

// ListDomains returns the domains containing channels in session.
func ListDomains(ctx context.Context, session string) ([]Domain, error) {
	var domains []Domain
	err := sessionCommand(ctx, "list domains", session, func(name *C.char) C.int {
		var ptr *C.struct_lttng_domain
		n := C.ctlgo_lttng_list_domains(name, &ptr)
		defer C.free(unsafe.Pointer(ptr))
		if n < 0 {
			return n
		}
		domains = make([]Domain, int(n))
		for i, domain := range unsafe.Slice(ptr, int(n)) {
			domains[i] = Domain{Type: DomainType(domain._type), BufferType: BufferType(domain.buf_type)}
		}
		return 0
	})
	return domains, err
}

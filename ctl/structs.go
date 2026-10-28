//go:build linux && cgo

package ctl

/*
#include <stdlib.h>
#include <string.h>
#include <lttng/lttng.h>
static void ctl_set_probe(struct lttng_event *e, uint64_t address, uint64_t offset, const char *symbol, const char *function) {
    memset(&e->attr, 0, sizeof(e->attr));
    if (e->type == LTTNG_EVENT_FUNCTION_ENTRY) { if (function) strcpy(e->attr.ftrace.symbol_name, function); }
    else { e->attr.probe.addr = address; e->attr.probe.offset = offset; if (symbol) strcpy(e->attr.probe.symbol_name, symbol); }
}
static const char *ctl_probe_symbol(struct lttng_event *e) { return e->attr.probe.symbol_name; }
static const char *ctl_function_symbol(struct lttng_event *e) { return e->attr.ftrace.symbol_name; }
static uint64_t ctl_probe_address(struct lttng_event *e) { return e->attr.probe.addr; }
static uint64_t ctl_probe_offset(struct lttng_event *e) { return e->attr.probe.offset; }
static void ctl_set_perf(struct lttng_event_context *c, uint32_t type, uint64_t config, const char *name) {
    memset(&c->u,0,sizeof(c->u)); c->u.perf_counter.type=type; c->u.perf_counter.config=config; if(name) strcpy(c->u.perf_counter.name,name);
}
static void ctl_set_app(struct lttng_event_context *c, char *provider, char *name) {
    memset(&c->u,0,sizeof(c->u)); c->u.app_ctx.provider_name=provider; c->u.app_ctx.ctx_name=name;
}
*/
import "C"

import (
	"context"
	"strings"
	"unsafe"

	"github.com/fanyang89/lttng-ctl-go/internal/ctlstate"
)

// DomainData contains the public fields used to select a tracing domain.
type DomainData struct {
	Type       DomainType
	BufferType BufferType
}

func NewDomain(ctx context.Context, data DomainData) (*Domain, error) {
	if err := ctlstate.Lock(ctx); err != nil {
		return nil, err
	}
	defer ctlstate.Unlock()
	p := (*C.struct_lttng_domain)(C.calloc(1, C.sizeof_struct_lttng_domain))
	if p == nil {
		return nil, ErrNull
	}
	p._type = C.enum_lttng_domain_type(data.Type)
	p.buf_type = C.enum_lttng_buffer_type(data.BufferType)
	return &Domain{ref: own(unsafe.Pointer(p), func(p unsafe.Pointer) { C.free(p) })}, nil
}

func (obj *Domain) Data(ctx context.Context) (DomainData, error) {
	if err := ctlstate.Lock(ctx); err != nil {
		return DomainData{}, err
	}
	defer ctlstate.Unlock()
	if err := obj.resource().check(false); err != nil {
		return DomainData{}, err
	}
	p := (*C.struct_lttng_domain)(obj.ref.ptr)
	return DomainData{Type: DomainType(p._type), BufferType: BufferType(p.buf_type)}, nil
}

// ChannelAttributes preserves native units (bytes, counts, and microseconds).
// Extended attributes are accessed with ChannelGet/Set functions.
type ChannelAttributes struct {
	Overwrite                     int32
	SubbufferSize, SubbufferCount uint64
	SwitchTimer, ReadTimer        uint32
	Output                        EventOutput
	TracefileSize, TracefileCount uint64
	LiveTimer                     uint32
}

type ChannelData struct {
	Name       string
	Enabled    uint32
	Attributes ChannelAttributes
}

func readAttributes(p *C.struct_lttng_channel_attr) ChannelAttributes {
	return ChannelAttributes{Overwrite: int32(p.overwrite), SubbufferSize: uint64(p.subbuf_size), SubbufferCount: uint64(p.num_subbuf), SwitchTimer: uint32(p.switch_timer_interval), ReadTimer: uint32(p.read_timer_interval), Output: EventOutput(p.output), TracefileSize: uint64(p.tracefile_size), TracefileCount: uint64(p.tracefile_count), LiveTimer: uint32(p.live_timer_interval)}
}

func writeAttributes(p *C.struct_lttng_channel_attr, data ChannelAttributes) {
	p.overwrite = C.int(data.Overwrite)
	p.subbuf_size = C.uint64_t(data.SubbufferSize)
	p.num_subbuf = C.uint64_t(data.SubbufferCount)
	p.switch_timer_interval = C.uint(data.SwitchTimer)
	p.read_timer_interval = C.uint(data.ReadTimer)
	p.output = C.enum_lttng_event_output(data.Output)
	p.tracefile_size = C.uint64_t(data.TracefileSize)
	p.tracefile_count = C.uint64_t(data.TracefileCount)
	p.live_timer_interval = C.uint(data.LiveTimer)
}

func NewChannelAttr(ctx context.Context, data ChannelAttributes) (*ChannelAttr, error) {
	if err := ctlstate.Lock(ctx); err != nil {
		return nil, err
	}
	defer ctlstate.Unlock()
	p := (*C.struct_lttng_channel_attr)(C.calloc(1, C.sizeof_struct_lttng_channel_attr))
	if p == nil {
		return nil, ErrNull
	}
	writeAttributes(p, data)
	return &ChannelAttr{ref: own(unsafe.Pointer(p), func(p unsafe.Pointer) { C.free(p) })}, nil
}

func (obj *ChannelAttr) Data(ctx context.Context) (ChannelAttributes, error) {
	if err := ctlstate.Lock(ctx); err != nil {
		return ChannelAttributes{}, err
	}
	defer ctlstate.Unlock()
	if err := obj.resource().check(false); err != nil {
		return ChannelAttributes{}, err
	}
	return readAttributes((*C.struct_lttng_channel_attr)(obj.ref.ptr)), nil
}

func (obj *Channel) Data(ctx context.Context) (ChannelData, error) {
	if err := ctlstate.Lock(ctx); err != nil {
		return ChannelData{}, err
	}
	defer ctlstate.Unlock()
	if err := obj.resource().check(false); err != nil {
		return ChannelData{}, err
	}
	p := (*C.struct_lttng_channel)(obj.ref.ptr)
	return ChannelData{Name: C.GoString(&p.name[0]), Enabled: uint32(p.enabled), Attributes: readAttributes(&p.attr)}, nil
}

func validName(value string, limit int) bool {
	return len(value) < limit && !strings.ContainsRune(value, 0)
}
func putName(dst []C.char, value string) {
	clear(dst)
	for i := range len(value) {
		dst[i] = C.char(value[i])
	}
}

func (obj *Channel) SetData(ctx context.Context, data ChannelData) error {
	if err := ctlstate.Lock(ctx); err != nil {
		return err
	}
	defer ctlstate.Unlock()
	if err := obj.resource().check(true); err != nil {
		return err
	}
	if !validName(data.Name, C.LTTNG_SYMBOL_NAME_LEN) {
		return ErrInvalidArgument
	}
	p := (*C.struct_lttng_channel)(obj.ref.ptr)
	putName(p.name[:], data.Name)
	p.enabled = C.uint32_t(data.Enabled)
	writeAttributes(&p.attr, data.Attributes)
	obj.ref.revision++
	return nil
}

// EventData exposes the legacy event structure, including kprobe/kretprobe and
// function-tracer union fields. Userspace locations use their dedicated API.
type EventData struct {
	Name                        string
	Type                        EventType
	LoglevelType                LoglevelType
	Loglevel                    int32
	Enabled, PID                int32
	Flags                       EventFlag
	ProbeAddress, ProbeOffset   uint64
	ProbeSymbol, FunctionSymbol string
}

func readEvent(p *C.struct_lttng_event) EventData {
	d := EventData{Name: C.GoString(&p.name[0]), Type: EventType(p._type), LoglevelType: LoglevelType(p.loglevel_type), Loglevel: int32(p.loglevel), Enabled: int32(p.enabled), PID: int32(p.pid), Flags: EventFlag(p.flags)}
	if p._type == C.LTTNG_EVENT_PROBE || p._type == C.LTTNG_EVENT_FUNCTION {
		d.ProbeAddress = uint64(C.ctl_probe_address(p))
		d.ProbeOffset = uint64(C.ctl_probe_offset(p))
		d.ProbeSymbol = C.GoString(C.ctl_probe_symbol(p))
	}
	if p._type == C.LTTNG_EVENT_FUNCTION_ENTRY {
		d.FunctionSymbol = C.GoString(C.ctl_function_symbol(p))
	}
	return d
}

func (obj *Event) Data(ctx context.Context) (EventData, error) {
	if err := ctlstate.Lock(ctx); err != nil {
		return EventData{}, err
	}
	defer ctlstate.Unlock()
	if err := obj.resource().check(false); err != nil {
		return EventData{}, err
	}
	return readEvent((*C.struct_lttng_event)(obj.ref.ptr)), nil
}

func (obj *Event) SetData(ctx context.Context, data EventData) error {
	if err := ctlstate.Lock(ctx); err != nil {
		return err
	}
	defer ctlstate.Unlock()
	if err := obj.resource().check(true); err != nil {
		return err
	}
	for _, name := range []string{data.Name, data.ProbeSymbol, data.FunctionSymbol} {
		if !validName(name, C.LTTNG_SYMBOL_NAME_LEN) {
			return ErrInvalidArgument
		}
	}
	p := (*C.struct_lttng_event)(obj.ref.ptr)
	putName(p.name[:], data.Name)
	p._type = C.enum_lttng_event_type(data.Type)
	p.loglevel_type = C.enum_lttng_loglevel_type(data.LoglevelType)
	p.loglevel = C.int(data.Loglevel)
	p.enabled = C.int32_t(data.Enabled)
	p.pid = C.pid_t(data.PID)
	p.flags = C.enum_lttng_event_flag(data.Flags)
	symbol, function := C.CString(data.ProbeSymbol), C.CString(data.FunctionSymbol)
	defer C.free(unsafe.Pointer(symbol))
	defer C.free(unsafe.Pointer(function))
	C.ctl_set_probe(p, C.uint64_t(data.ProbeAddress), C.uint64_t(data.ProbeOffset), symbol, function)
	obj.ref.revision++
	return nil
}

// ContextData supports basic, perf counter, and application-specific contexts.
type ContextData struct {
	Type                                EventContextType
	PerfType                            uint32
	PerfConfig                          uint64
	PerfName, ProviderName, ContextName string
}

func NewEventContext(ctx context.Context, data ContextData) (*EventContext, error) {
	if err := ctlstate.Lock(ctx); err != nil {
		return nil, err
	}
	defer ctlstate.Unlock()
	if !validName(data.PerfName, C.LTTNG_SYMBOL_NAME_LEN) || !validName(data.ProviderName, 1<<30) || !validName(data.ContextName, 1<<30) {
		return nil, ErrInvalidArgument
	}
	p := (*C.struct_lttng_event_context)(C.calloc(1, C.sizeof_struct_lttng_event_context))
	if p == nil {
		return nil, ErrNull
	}
	ref := own(unsafe.Pointer(p), func(p unsafe.Pointer) { C.free(p) })
	p.ctx = C.enum_lttng_event_context_type(data.Type)
	if data.Type == EventContextAppContext {
		provider, name := C.CString(data.ProviderName), C.CString(data.ContextName)
		ref.extra = []unsafe.Pointer{unsafe.Pointer(provider), unsafe.Pointer(name)}
		C.ctl_set_app(p, provider, name)
	} else if data.Type == EventContextPerfCounter || data.Type == EventContextPerfCPUCounter || data.Type == EventContextPerfThreadCounter {
		name := C.CString(data.PerfName)
		defer C.free(unsafe.Pointer(name))
		C.ctl_set_perf(p, C.uint32_t(data.PerfType), C.uint64_t(data.PerfConfig), name)
	}
	return &EventContext{ref: ref}, nil
}

func NewCalibration(ctx context.Context, kind CalibrateType) (*Calibration, error) {
	if err := ctlstate.Lock(ctx); err != nil {
		return nil, err
	}
	defer ctlstate.Unlock()
	p := (*C.struct_lttng_calibrate)(C.calloc(1, C.sizeof_struct_lttng_calibrate))
	if p == nil {
		return nil, ErrNull
	}
	p._type = C.enum_lttng_calibrate_type(kind)
	return &Calibration{ref: own(unsafe.Pointer(p), func(p unsafe.Pointer) { C.free(p) })}, nil
}

type SessionData struct {
	Name, Path                       string
	Enabled, SnapshotMode, LiveTimer uint32
}

func (obj *Session) Data(ctx context.Context) (SessionData, error) {
	if err := ctlstate.Lock(ctx); err != nil {
		return SessionData{}, err
	}
	defer ctlstate.Unlock()
	if err := obj.resource().check(false); err != nil {
		return SessionData{}, err
	}
	p := (*C.struct_lttng_session)(obj.ref.ptr)
	return SessionData{Name: C.GoString(&p.name[0]), Path: C.GoString(&p.path[0]), Enabled: uint32(p.enabled), SnapshotMode: uint32(p.snapshot_mode), LiveTimer: uint32(p.live_timer_interval)}, nil
}

type EventFieldData struct {
	Name    string
	Type    EventFieldType
	NoWrite bool
	Event   EventData
}

func (obj *EventField) Data(ctx context.Context) (EventFieldData, error) {
	if err := ctlstate.Lock(ctx); err != nil {
		return EventFieldData{}, err
	}
	defer ctlstate.Unlock()
	if err := obj.resource().check(false); err != nil {
		return EventFieldData{}, err
	}
	p := (*C.struct_lttng_event_field)(obj.ref.ptr)
	return EventFieldData{Name: C.GoString(&p.field_name[0]), Type: EventFieldType(p._type), NoWrite: p.nowrite != 0, Event: readEvent(&p.event)}, nil
}

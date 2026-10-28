//go:build linux && cgo

package lttng

/*
#include <stdlib.h>
#include "internal/ctlstate/bridge_generated.h"
*/
import "C"

import (
	"context"
	"math"
	"time"
	"unsafe"
)

// LossMode selects how full buffers are handled.
type LossMode int

const (
	LossDefault   LossMode = -1 // Use the session's default.
	LossDiscard   LossMode = 0  // Discard new events.
	LossOverwrite LossMode = 1  // Overwrite old packets.
)

// OutputType selects how the consumer reads channel buffers.
type OutputType int

const (
	OutputSplice OutputType = C.LTTNG_EVENT_SPLICE
	OutputMmap   OutputType = C.LTTNG_EVENT_MMAP
)

// ChannelConfig contains the basic attributes of a channel. Obtain a starting
// value with DefaultChannelConfig, then modify the desired fields. Timers must
// be non-negative whole microseconds fitting an unsigned 32-bit integer.
type ChannelConfig struct {
	LossMode       LossMode
	SubbufferSize  uint64 // Bytes, power of two.
	SubbufferCount uint64 // Power of two.
	SwitchTimer    time.Duration
	ReadTimer      time.Duration
	Output         OutputType
	TracefileSize  uint64 // Bytes; zero means unlimited.
	TracefileCount uint64 // Zero means unlimited.
}

// Channel is a point-in-time summary. The loss counter corresponding to
// Config.LossMode is populated; the other counter is zero.
type Channel struct {
	Name            string
	Enabled         bool
	Config          ChannelConfig
	DiscardedEvents uint64
	LostPackets     uint64
}

// DefaultChannelConfig obtains defaults from liblttng-ctl, without contacting
// sessiond. Extended attributes not exposed by ChannelConfig retain defaults.
func DefaultChannelConfig(ctx context.Context, domain Domain) (ChannelConfig, error) {
	const op = "default channel config"
	if err := domain.validate(op); err != nil {
		return ChannelConfig{}, err
	}
	if err := lock(ctx); err != nil {
		return ChannelConfig{}, err
	}
	defer unlock()
	nativeDomain := domain.native()
	channel := createChannel(&nativeDomain)
	if channel == nil {
		return ChannelConfig{}, nativeError(op, -C.LTTNG_ERR_NOMEM)
	}
	defer destroyChannel(channel)
	return channelConfig(&channel.attr), nil
}

// LTTng 2.8 exposes zero-initialized public channels and the defaults setter,
// but not the allocation API introduced in 2.9.
func createChannel(domain *C.struct_lttng_domain) *C.struct_lttng_channel {
	if nativeHas("lttng_channel_create") && nativeHas("lttng_channel_destroy") {
		return C.ctlgo_lttng_channel_create(domain)
	}
	channel := (*C.struct_lttng_channel)(C.calloc(1, C.sizeof_struct_lttng_channel))
	if channel != nil {
		C.ctlgo_lttng_channel_set_default_attr(domain, &channel.attr)
		if nativeError("channel defaults", 0) != nil {
			C.free(unsafe.Pointer(channel))
			return nil
		}
	}
	return channel
}

func destroyChannel(channel *C.struct_lttng_channel) {
	if nativeHas("lttng_channel_create") && nativeHas("lttng_channel_destroy") {
		C.ctlgo_lttng_channel_destroy(channel)
	} else {
		C.free(unsafe.Pointer(channel))
	}
}

func channelConfig(attr *C.struct_lttng_channel_attr) ChannelConfig {
	return ChannelConfig{
		LossMode: LossMode(attr.overwrite), SubbufferSize: uint64(attr.subbuf_size),
		SubbufferCount: uint64(attr.num_subbuf), Output: OutputType(attr.output),
		SwitchTimer:   time.Duration(attr.switch_timer_interval) * time.Microsecond,
		ReadTimer:     time.Duration(attr.read_timer_interval) * time.Microsecond,
		TracefileSize: uint64(attr.tracefile_size), TracefileCount: uint64(attr.tracefile_count),
	}
}

func (config ChannelConfig) validate(op string) error {
	if config.LossMode < LossDefault || config.LossMode > LossOverwrite {
		return invalid(op, "invalid loss mode")
	}
	if config.SubbufferSize == 0 || config.SubbufferSize&(config.SubbufferSize-1) != 0 ||
		config.SubbufferCount == 0 || config.SubbufferCount&(config.SubbufferCount-1) != 0 {
		return invalid(op, "sub-buffer size and count must be non-zero powers of two")
	}
	if config.Output != OutputMmap && config.Output != OutputSplice {
		return invalid(op, "invalid channel output type")
	}
	for _, timer := range []time.Duration{config.SwitchTimer, config.ReadTimer} {
		if timer < 0 || timer%time.Microsecond != 0 || timer/time.Microsecond > math.MaxUint32 {
			return invalid(op, "channel timers must be whole microseconds in the uint32 range")
		}
	}
	return nil
}

// EnableChannel creates or re-enables a channel. A nil config selects official
// defaults. For an existing channel, liblttng-ctl ignores configuration changes.
// New channels must be configured before the session is first started.
func EnableChannel(ctx context.Context, session string, domain Domain, name string, config *ChannelConfig) error {
	const op = "enable channel"
	if err := checkString(op, "channel name", name, C.LTTNG_SYMBOL_NAME_LEN, false); err != nil {
		return err
	}
	if config != nil {
		if err := config.validate(op); err != nil {
			return err
		}
	}
	return withHandle(ctx, op, session, domain, func(handle *C.struct_lttng_handle) error {
		nativeDomain := domain.native()
		channel := createChannel(&nativeDomain)
		if channel == nil {
			return nativeError(op, -C.LTTNG_ERR_NOMEM)
		}
		defer destroyChannel(channel)
		copyName(channel.name[:], name)
		if config != nil {
			channel.attr.overwrite = C.int(config.LossMode)
			channel.attr.subbuf_size = C.uint64_t(config.SubbufferSize)
			channel.attr.num_subbuf = C.uint64_t(config.SubbufferCount)
			channel.attr.switch_timer_interval = C.uint(config.SwitchTimer / time.Microsecond)
			channel.attr.read_timer_interval = C.uint(config.ReadTimer / time.Microsecond)
			channel.attr.output = C.enum_lttng_event_output(config.Output)
			channel.attr.tracefile_size = C.uint64_t(config.TracefileSize)
			channel.attr.tracefile_count = C.uint64_t(config.TracefileCount)
		}
		return nativeError(op, C.ctlgo_lttng_enable_channel(handle, channel))
	})
}

// DisableChannel disables a named channel without destroying its configuration.
func DisableChannel(ctx context.Context, session string, domain Domain, name string) error {
	const op = "disable channel"
	if err := checkString(op, "channel name", name, C.LTTNG_SYMBOL_NAME_LEN, false); err != nil {
		return err
	}
	return withHandle(ctx, op, session, domain, func(handle *C.struct_lttng_handle) error {
		name := C.CString(name)
		defer free(name)
		return nativeError(op, C.ctlgo_lttng_disable_channel(handle, name))
	})
}

// ListChannels returns channels and their loss counters in a session and domain.
func ListChannels(ctx context.Context, session string, domain Domain) ([]Channel, error) {
	const op = "list channels"
	var channels []Channel
	err := withHandle(ctx, op, session, domain, func(handle *C.struct_lttng_handle) error {
		var ptr *C.struct_lttng_channel
		n := C.ctlgo_lttng_list_channels(handle, &ptr)
		defer C.free(unsafe.Pointer(ptr))
		if err := nativeError(op, n); err != nil {
			return err
		}
		channels = make([]Channel, int(n))
		for i := range int(n) {
			native := &unsafe.Slice(ptr, int(n))[i]
			channel := Channel{Name: C.GoString(&native.name[0]), Enabled: native.enabled != 0, Config: channelConfig(&native.attr)}
			var count C.uint64_t
			var result C.int
			if channel.Config.LossMode == LossOverwrite {
				result = C.ctlgo_lttng_channel_get_lost_packet_count(native, &count)
				channel.LostPackets = uint64(count)
			} else {
				result = C.ctlgo_lttng_channel_get_discarded_event_count(native, &count)
				channel.DiscardedEvents = uint64(count)
			}
			if err := nativeError(op, result); err != nil {
				return err
			}
			channels[i] = channel
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return channels, nil
}

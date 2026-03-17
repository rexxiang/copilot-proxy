package core

const (
	// KernelEventTypeStart marks the kernel lifecycle entering running state.
	KernelEventTypeStart = "kernel.start"
	// KernelEventTypeStop marks the kernel lifecycle transitioning to stopped.
	KernelEventTypeStop = "kernel.stop"
	// KernelEventTypeInvoke signals an in-process invocation.
	KernelEventTypeInvoke = "kernel.invoke"
)

//go:build linux && amd64

#include "textflag.h"

// func clientRequest(args *[6]uint64) uint64
//
// The client request of valgrind on amd64: AX holds the address of the arguments and DX the
// result outside valgrind. The four rotations of DI add up to a whole turn, and the exchange of
// BX with itself changes nothing, so the sequence does nothing when valgrind does not run the
// program.
TEXT ·clientRequest(SB), NOSPLIT, $0-16
	MOVQ args+0(FP), AX
	MOVQ $0, DX
	ROLQ $3, DI
	ROLQ $13, DI
	ROLQ $61, DI
	ROLQ $51, DI
	XCHGQ BX, BX
	MOVQ DX, ret+8(FP)
	RET

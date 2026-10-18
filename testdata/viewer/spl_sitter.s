; SPDX-License-Identifier: AGPL-3.0-or-later
; Parity fixture bot: one cell that jumps to itself, never writes, never dies.
here:   JMP.B  here, $0

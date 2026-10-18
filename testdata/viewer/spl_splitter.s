; SPDX-License-Identifier: AGPL-3.0-or-later
; Parity fixture bot: two SPLs, a process that dies, a looping process and an
; imp-like process that writes. Start of testdata/viewer/spl_fixture.jsonl.
        SPL.B  sit, $0       ; cell 0: spawn at cell 3
        SPL.B  imp, $0       ; cell 1: spawn at cell 4
        DAT.F  $0, $0        ; cell 2: this process dies
sit:    JMP.B  sit, $0       ; cell 3: loops on itself
imp:    MOV.I  $0, $1        ; cell 4: copies itself forward

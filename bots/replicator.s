; replicator: copies its eight code cells 100 cells ahead, starts the copy, and goes
; round again 100 cells further on, so that every copy copies itself too.
; ptr.A is the source and ptr.B the destination, both as offsets from ptr. The
; cell after the code (ptr) is not copied, so a new copy finds ptr empty and sets
; its first destination itself: ptr + 92, which puts the eight copied cells 100 to
; 107 cells past the start of the code. Once a pass has copied, ptr.B points at the
; copy's ptr; SUB makes it point at the copy's first cell, which SPL starts, and
; ADD moves it 100 on for the next pass. The original therefore never dies: it
; copies to 100, 200, 300, ... while each copy does the same, so the number of
; processes grows. Copies meet on the same 100-cell sites (the copy at 100 sends
; its first copy to 200, where the original's second pass also goes), and processes
; in one copy share its ptr, so some of them aim at garbage and die. The rest go on.
; Original work for pitfight (AGPL-3.0-or-later, see ../NOTICE).

start:  MOV.AB  #92, ptr        ; 0: a new copy starts here: first destination
again:  MOV.A   #-8, ptr        ; 1: source = start of the code
loop:   MOV.I   }ptr, >ptr      ; 2: copy a cell, step both
        JMN.A   loop, ptr       ; 3: until the source reaches ptr
        SUB.AB  #8, ptr         ; 4: ptr.B = offset of the copy's first cell
        SPL     @ptr            ; 5: start the copy at its first cell
        ADD.AB  #100, ptr       ; 6: next destination, 100 cells further
        JMP     again           ; 7: copy again
ptr:    DAT.F   $0, $0          ; 8: A = source offset, B = destination offset

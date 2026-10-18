; dwarf: a bomber that drops a DAT bomb on every fourth cell.
; The B field of the pointer cell is the distance from the pointer cell to the
; next target. Each loop adds 4 to it and copies the bomb cell onto the target,
; 3 cycles per bomb, the first bomb 4 cells past the pointer. The targets are the
; cells 3 + 4k from the start. With the default core size 8000 (a multiple of 4)
; none of them is one of the dwarf's own code cells (0 to 2) or its bomb (4); the
; pointer cell (3) comes round once per lap and is only reset to zero.
; Original work for pitfight (AGPL-3.0-or-later, see ../NOTICE).

start:  ADD.AB  #4, ptr         ; 0: step the target distance
        MOV.I   bomb, @ptr      ; 1: bomb the cell ptr + distance
        JMP     start           ; 2: again
ptr:    DAT.F   #0, #0          ; 3: B = distance to the target
bomb:   DAT.F   #0, #0          ; 4: the bomb

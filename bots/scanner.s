; scanner: looks at every third cell for one that is not empty and bombs it.
; The B field of the pointer cell is the distance from the pointer cell to the cell
; being examined; it grows by 3 each pass. A lap is 2600 passes and examines the
; cells 10 to 7807 from the start of the code, which stops short of the empty gap
; that placement leaves in front of the code (cells 7900 to 7999), so the scanner
; never examines its own cells. Blind spot: enemy code that lies only in cells
; 7808 to 7899 (placement allows it) is never examined. After a lap it starts again.
; The blank cell is both the reference (an empty cell) and the bomb: a bombed cell
; looks empty and is not found again, and a process that runs it dies.
; Original work for pitfight (AGPL-3.0-or-later, see ../NOTICE).

scan:   ADD.AB  #3, ptr         ; 0: next cell
        SEQ.I   @ptr, blank     ; 1: skip the bomb if that cell is empty
        MOV.I   blank, @ptr     ; 2: not empty: bomb it
        DJN     scan, count     ; 3: count the pass, scan on
        MOV.AB  #2600, count    ; 4: lap done: reset the pass counter
        MOV.AB  #0, ptr         ; 5: and the pointer
        JMP     scan            ; 6: next lap
ptr:    DAT.F   $0, $0          ; 7: B = distance to the examined cell
count:  DAT.F   $0, #2600       ; 8: B = passes left in this lap
blank:  DAT.F   $0, $0          ; 9: reference and bomb

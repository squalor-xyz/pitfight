; imp: a one-cell bot that crawls through the core.
; Each step copies the whole cell onto the next cell and continues into the copy,
; so the single process moves one cell per cycle and leaves a trail of imps behind
; it. Nothing ever kills it by itself: it only dies when another bot writes a
; bomb on the cell it is about to run.
; Original work for pitfight (AGPL-3.0-or-later, see ../NOTICE).

        MOV.I   $0, $1

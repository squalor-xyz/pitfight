// SPDX-License-Identifier: AGPL-3.0-or-later

package replay

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"

	"github.com/squalor-xyz/pitfight/internal/arena"
)

// Hash returns the SHA-256 of data as 64 lowercase hexadecimal characters. The
// replay hash is Hash of the bytes of the replay file (docs/spec/replay.md
// section 6).
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// BotHash returns the bot hash of docs/spec/replay.md section 6: the SHA-256 of the
// entry offset (4 bytes, big endian) followed by 12 bytes per cell as the assembler
// produced it: opcode, modifier, a_mode, a_value (int32), b_mode, b_value (int32),
// all big endian. The cell's writer is not hashed.
func BotHash(b arena.Bot) string {
	buf := make([]byte, 0, 4+12*len(b.Cells))
	buf = binary.BigEndian.AppendUint32(buf, uint32(b.Entry))
	for _, c := range b.Cells {
		buf = append(buf, byte(c.Opcode), byte(c.Modifier), byte(c.AMode))
		buf = binary.BigEndian.AppendUint32(buf, uint32(c.AValue))
		buf = append(buf, byte(c.BMode))
		buf = binary.BigEndian.AppendUint32(buf, uint32(c.BValue))
	}
	return Hash(buf)
}

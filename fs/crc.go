package fs

// CRC16 computes a seedable CRC-16/ARC (poly 0xA001, init 0x0000, reflected,
// no final XOR), matching openant.fs.commons.crc. The seed allows chained
// CRC computation across download/upload blocks.
func CRC16(data []byte, seed uint16) uint16 {
	rem := seed
	for _, b := range data {
		rem ^= uint16(b)
		for i := 0; i < 8; i++ {
			if rem&1 != 0 {
				rem = (rem >> 1) ^ 0xA001
			} else {
				rem >>= 1
			}
		}
	}
	return rem
}

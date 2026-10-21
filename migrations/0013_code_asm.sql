-- the code's TVM assembly, from the official fift disassembler (Disasm.fif)
-- bundled in the image; filled in the background after code_boc (see asm.go).
-- asm_error is set instead when fift can't disassemble it (e.g. a library cell).
ALTER TABLE ton.code
    ADD COLUMN IF NOT EXISTS asm String CODEC(ZSTD(9)),
    ADD COLUMN IF NOT EXISTS asm_error String;

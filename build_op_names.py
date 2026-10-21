#!/usr/bin/env python3
"""Regenerate op_names_registry.tsv from the public TON ABI registries.

    git clone --depth 1 https://github.com/ton-blockchain/abis.git /tmp/abis
    curl -sLo /tmp/messages.md https://raw.githubusercontent.com/tonkeeper/tongo/master/abi/messages.md
    python3 build_op_names.py /tmp/abis /tmp/messages.md

Sources, in order of preference:
  * ton-blockchain/abis: `struct (0x...) Name` in data/<protocol>/**.tolk, with
    the `///` doc comment above it as the description
  * tonkeeper/tongo abi/messages.md: the `| Name| 0x... |` table

Ops already in the hand-checked op_names.tsv are skipped, so that file wins.
When several protocols use the same op, every name is listed and the
description is left empty -- no single one would be right.
"""
import collections
import glob
import os
import re
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent


def main(abis_dir, tongo_md):
    curated = {int(line.split("\t")[0], 0)
               for line in (HERE / "op_names.tsv").read_text().splitlines()[1:] if line.strip()}

    abis = collections.defaultdict(list)  # op -> [(protocol, name, url, doc)]
    for f in sorted(glob.glob(os.path.join(abis_dir, "data", "**", "*.tolk"), recursive=True)):
        rel = os.path.relpath(f, abis_dir)
        protocol = rel.split(os.sep)[1]
        text = Path(f).read_text(encoding="utf-8")
        for m in re.finditer(r"((?:^///[^\n]*\n)*)struct \((0x[0-9a-fA-F]{8})\) (\w+)", text, re.M):
            doc = " ".join(x[3:].strip() for x in m.group(1).splitlines()).strip().replace("\t", " ")
            abis[int(m.group(2), 16)].append(
                (protocol, m.group(3), "https://github.com/ton-blockchain/abis/blob/main/" + rel, doc))

    tongo = collections.defaultdict(set)
    for m in re.finditer(r"^\| (\w+)\| (0x[0-9a-fA-F]{8}) \|", Path(tongo_md).read_text(encoding="utf-8"), re.M):
        tongo[int(m.group(2), 16)].add(m.group(1))

    out = []
    for op in sorted(set(abis) | set(tongo)):
        if op in curated:
            continue
        if op in abis:
            entries = abis[op]
            protocols = sorted({e[0] for e in entries})
            names = sorted({e[1] for e in entries})
            name = names[0] if len(names) == 1 else " / ".join(names[:3]) + (" …" if len(names) > 3 else "")
            doc = next((e[3] for e in entries if e[3]), "") if len(protocols) == 1 else ""
            out.append("0x%08x\t%s\t%s\t%s\t%s" % (op, name, ", ".join(protocols[:3]), doc, entries[0][2]))
        else:
            out.append("0x%08x\t%s\t\t\thttps://github.com/tonkeeper/tongo/blob/master/abi/messages.md"
                       % (op, " / ".join(sorted(tongo[op]))))

    (HERE / "op_names_registry.tsv").write_text(
        "op\tname\tprotocol\tdescription\tsource\n" + "\n".join(out) + "\n", encoding="utf-8")
    print("%d registry rows" % len(out))


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])

# -*- coding: utf-8 -*-
"""PDF 嵌入 CFF（CIDFontType0C）交叉验证工具（测试辅助，需 fontTools）。

用法: python cffcheck.py <pdf> [<pdf>...]
退出码: 0 全部通过；1 校验失败；2 环境缺 fontTools（调用方据此跳过测试）。
"""
import io
import re
import sys
import zlib

try:
    from fontTools import cffLib
    from fontTools.pens.recordingPen import RecordingPen
except ImportError:
    sys.exit(2)


def find_streams(pdf):
    """粗略提取所有间接对象的 (字典, 流数据)。"""
    out = []
    for m in re.finditer(rb'(\d+)\s+0\s+obj\s*(<<.*?>>)\s*stream\r?\n', pdf, re.S):
        start = m.end()
        end = pdf.find(b'endstream', start)
        if end < 0:
            continue
        out.append((m.group(2), pdf[start:end]))
    return out


def decode_literal(body):
    """解码 PDF 括号字符串（含八进制与命名转义）为字节。"""
    out = bytearray()
    i = 0
    while i < len(body):
        if body[i:i + 1] == b'\\':
            m8 = re.match(rb'[0-7]{1,3}', body[i + 1:])
            if m8:
                out.append(int(m8.group(0), 8) & 0xFF)
                i += 1 + len(m8.group(0))
                continue
            esc = body[i + 1:i + 2]
            out.append({b'n': 10, b'r': 13, b't': 9, b'b': 8, b'f': 12}.get(
                esc, esc[0] if esc else 0))
            i += 2
        else:
            out.append(body[i])
            i += 1
    return bytes(out)


def check(path):
    """校验单个 PDF：结构标记、嵌入 CFF 完整性、ToUnicode 文本反查。返回提取的文本。"""
    pdf = open(path, 'rb').read()
    for mark in (b'/CIDFontType0', b'/CIDFontType0C', b'/FontFile3', b'/ToUnicode'):
        if mark not in pdf:
            raise SystemExit('%s: 缺少 %s' % (path, mark.decode()))
    if b'/CIDToGIDMap' in pdf:
        raise SystemExit('%s: CIDFontType0 不应含 /CIDToGIDMap' % path)

    to_uni = None
    cff_glyphs = 0
    for d, data in find_streams(pdf):
        raw = data.rstrip(b'\r\n')
        if b'/FlateDecode' in d:
            try:
                raw = zlib.decompress(raw)
            except Exception:
                continue
        if b'/CIDFontType0C' in d:
            fs = cffLib.CFFFontSet()
            fs.decompile(io.BytesIO(raw), None)
            font = fs[list(fs.keys())[0]]
            for gn in font.getGlyphOrder():
                cs = font.CharStrings[gn]
                cs.decompile()  # 执行 subr 调用链，引用错误会抛异常
                pen = RecordingPen()
                cs.draw(pen)
                cff_glyphs += 1
        elif b'begincmap' in raw:
            to_uni = raw
    if cff_glyphs == 0:
        raise SystemExit('%s: 未找到嵌入 CFF 流' % path)
    if to_uni is None:
        raise SystemExit('%s: 未找到 ToUnicode 流' % path)
    print('%s: 嵌入 CFF %d 字形校验通过' % (path, cff_glyphs))

    mapping = {}
    for m in re.finditer(rb'<([0-9A-Fa-f]{4})>\s*<([0-9A-Fa-f]+)>', to_uni):
        mapping[int(m.group(1), 16)] = ''.join(
            chr(int(m.group(2)[i:i + 4], 16)) for i in range(0, len(m.group(2)), 4))
    texts = []
    for d, data in find_streams(pdf):
        if b'/FlateDecode' not in d:
            continue
        try:
            raw = zlib.decompress(data.rstrip(b'\r\n'))
        except Exception:
            continue
        for hm in re.finditer(rb'<([0-9A-Fa-f]+)>\s*Tj', raw):
            hexs = hm.group(1)
            texts.append(''.join(mapping.get(int(hexs[i:i + 4], 16), '?')
                                 for i in range(0, len(hexs), 4)))
        for sm in re.finditer(rb'\(((?:[^()\\]|\\.)*)\)\s*Tj', raw):
            out = decode_literal(sm.group(1))
            texts.append(''.join(mapping.get(out[j] << 8 | out[j + 1], '?')
                                 for j in range(0, len(out) - 1, 2)))
        for am in re.finditer(rb'\[(.*?)\]\s*TJ', raw, re.S):
            s = ''
            for hm in re.finditer(rb'<([0-9A-Fa-f]+)>', am.group(1)):
                hexs = hm.group(1)
                s += ''.join(mapping.get(int(hexs[i:i + 4], 16), '?')
                             for i in range(0, len(hexs), 4))
            texts.append(s)
    return ''.join(texts)


def main():
    for path in sys.argv[1:]:
        text = check(path)
        print('TEXT:' + text)
    print('PASS')


if __name__ == '__main__':
    main()

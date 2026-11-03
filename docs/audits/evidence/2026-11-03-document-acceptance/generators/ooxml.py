# Minimal, standards-shaped OOXML writers built only on the Python stdlib.
# Used to generate acceptance fixtures for the AirReply macOS document importer.
import base64
import struct
import zipfile
import zlib
from xml.sax.saxutils import escape, quoteattr

NS_MAIN = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
NS_R = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
NS_PKG = "http://schemas.openxmlformats.org/package/2006/relationships"
NS_W = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
REL = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
XML_DECL = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n'


# ---------------------------------------------------------------- cells
class Cell:
    pass


class S(Cell):  # shared string
    def __init__(self, text):
        self.text = text


class I(Cell):  # inline string
    def __init__(self, text):
        self.text = text


class N(Cell):  # number (raw lexical value as written to <v>)
    def __init__(self, value, style=None):
        self.value = value if isinstance(value, str) else repr(value)
        self.style = style


class F(Cell):  # formula with cached value
    def __init__(self, formula, cached, style=None, t=None):
        self.formula = formula
        self.cached = cached if isinstance(cached, str) else repr(cached)
        self.style = style
        self.t = t


class B(Cell):
    def __init__(self, value):
        self.value = "1" if value else "0"


class E(Cell):
    def __init__(self, value):
        self.value = value


STYLE_PERCENT, STYLE_DATE, STYLE_TENGE = 1, 2, 3

STYLES = (XML_DECL +
          f'<styleSheet xmlns="{NS_MAIN}">'
          '<numFmts count="1"><numFmt numFmtId="164" formatCode="#,##0&quot; ₸&quot;"/></numFmts>'
          '<fonts count="1"><font><sz val="11"/><name val="Calibri"/></font></fonts>'
          '<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>'
          '<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>'
          '<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>'
          '<cellXfs count="4">'
          '<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>'
          '<xf numFmtId="9" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>'
          '<xf numFmtId="14" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>'
          '<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>'
          '</cellXfs>'
          '<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>'
          '</styleSheet>')


def col_letter(index):  # 0-based
    letters = ""
    index += 1
    while index:
        index, rem = divmod(index - 1, 26)
        letters = chr(65 + rem) + letters
    return letters


def sheet_xml(rows, shared, inline):
    out = [XML_DECL, f'<worksheet xmlns="{NS_MAIN}" xmlns:r="{NS_R}"><sheetViews><sheetView workbookViewId="0"/></sheetViews>'
           '<sheetFormatPr defaultRowHeight="15"/><sheetData>']
    number = 0
    for spec in rows:
        if isinstance(spec, tuple):
            number, cells = spec
        else:
            number += 1
            cells = spec
        out.append(f'<row r="{number}">')
        for column, cell in enumerate(cells):
            if cell is None:
                continue
            if isinstance(cell, str):
                cell = I(cell) if inline else S(cell)
            elif isinstance(cell, (int, float)):
                cell = N(cell)
            ref = f"{col_letter(column)}{number}"
            if isinstance(cell, S):
                if cell.text not in shared:
                    shared[cell.text] = len(shared)
                out.append(f'<c r="{ref}" t="s"><v>{shared[cell.text]}</v></c>')
            elif isinstance(cell, I):
                out.append(f'<c r="{ref}" t="inlineStr"><is><t xml:space="preserve">{escape(cell.text)}</t></is></c>')
            elif isinstance(cell, N):
                style = f' s="{cell.style}"' if cell.style else ""
                out.append(f'<c r="{ref}"{style}><v>{cell.value}</v></c>')
            elif isinstance(cell, F):
                style = f' s="{cell.style}"' if cell.style else ""
                t = f' t="{cell.t}"' if cell.t else ""
                out.append(f'<c r="{ref}"{style}{t}><f>{escape(cell.formula)}</f><v>{escape(cell.cached)}</v></c>')
            elif isinstance(cell, B):
                out.append(f'<c r="{ref}" t="b"><v>{cell.value}</v></c>')
            elif isinstance(cell, E):
                out.append(f'<c r="{ref}" t="e"><v>{escape(cell.value)}</v></c>')
            else:
                raise TypeError(cell)
        out.append("</row>")
    out.append("</sheetData></worksheet>")
    return "".join(out)


def shared_xml(shared):
    items = sorted(shared.items(), key=lambda kv: kv[1])
    body = "".join(f'<si><t xml:space="preserve">{escape(text)}</t></si>' for text, _ in items)
    return XML_DECL + f'<sst xmlns="{NS_MAIN}" count="{len(items)}" uniqueCount="{len(items)}">{body}</sst>'


def xlsx_parts(sheets, inline=False, abs_target=False, chartsheet=False, rprefix="r"):
    """Returns an ordered list of (name, bytes) for a workbook."""
    shared = {}
    sheet_parts = [(f"xl/worksheets/sheet{i + 1}.xml", sheet_xml(rows, shared, inline)) for i, (_, rows) in enumerate(sheets)]
    overrides = ['<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>',
                 '<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>']
    overrides += [f'<Override PartName="/{name}" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>'
                  for name, _ in sheet_parts]
    if shared:
        overrides.append('<Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/>')
    sheet_entries, rels = [], []
    for i, (name, _) in enumerate(sheets):
        sheet_entries.append(f'<sheet name={quoteattr(name)} sheetId="{i + 1}" {rprefix}:id="rId{i + 1}"/>')
        target = f"/xl/worksheets/sheet{i + 1}.xml" if abs_target else f"worksheets/sheet{i + 1}.xml"
        rels.append(f'<Relationship Id="rId{i + 1}" Type="{REL}worksheet" Target="{target}"/>')
    extra = []
    if chartsheet:
        n = len(sheets) + 1
        sheet_entries.append(f'<sheet name="Диаграмма" sheetId="{n}" {rprefix}:id="rId{n}"/>')
        rels.append(f'<Relationship Id="rId{n}" Type="{REL}chartsheet" Target="chartsheets/sheet1.xml"/>')
        overrides.append('<Override PartName="/xl/chartsheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.chartsheet+xml"/>')
        extra.append(("xl/chartsheets/sheet1.xml", XML_DECL + f'<chartsheet xmlns="{NS_MAIN}" xmlns:r="{NS_R}"><sheetViews><sheetView workbookViewId="0"/></sheetViews></chartsheet>'))
    base = len(sheets) + (2 if chartsheet else 1)
    rels.append(f'<Relationship Id="rId{base}" Type="{REL}styles" Target="styles.xml"/>')
    if shared:
        rels.append(f'<Relationship Id="rId{base + 1}" Type="{REL}sharedStrings" Target="sharedStrings.xml"/>')
    rns = f'xmlns:{rprefix}="{NS_R}"'
    parts = [
        ("[Content_Types].xml", XML_DECL + '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">'
         '<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>'
         '<Default Extension="xml" ContentType="application/xml"/>' + "".join(overrides) + "</Types>"),
        ("_rels/.rels", XML_DECL + f'<Relationships xmlns="{NS_PKG}"><Relationship Id="rId1" Type="{REL}officeDocument" Target="xl/workbook.xml"/></Relationships>'),
        ("xl/workbook.xml", XML_DECL + f'<workbook xmlns="{NS_MAIN}" {rns}><bookViews><workbookView/></bookViews><sheets>' + "".join(sheet_entries) + "</sheets></workbook>"),
        ("xl/_rels/workbook.xml.rels", XML_DECL + f'<Relationships xmlns="{NS_PKG}">' + "".join(rels) + "</Relationships>"),
        ("xl/styles.xml", STYLES),
    ]
    parts += sheet_parts + extra
    if shared:
        parts.append(("xl/sharedStrings.xml", shared_xml(shared)))
    return [(name, data.encode("utf-8") if isinstance(data, str) else data) for name, data in parts]


def write_zip(path, parts, compression=zipfile.ZIP_DEFLATED):
    with zipfile.ZipFile(path, "w", compression=compression) as archive:
        for name, data in parts:
            archive.writestr(name, data)


def write_xlsx(path, sheets, **options):
    write_zip(path, xlsx_parts(sheets, **options))


# ---------------------------------------------------------------- docx
def w_par(text, style=None):
    ppr = f'<w:pPr><w:pStyle w:val="{style}"/></w:pPr>' if style else ""
    return f'<w:p>{ppr}<w:r><w:t xml:space="preserve">{escape(text)}</w:t></w:r></w:p>'


def w_table(rows):
    out = ['<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="0" w:type="auto"/></w:tblPr>']
    for row in rows:
        out.append("<w:tr>")
        for value in row:
            out.append(f"<w:tc><w:tcPr><w:tcW w:w=\"3000\" w:type=\"dxa\"/></w:tcPr>{w_par(value)}</w:tc>")
        out.append("</w:tr>")
    out.append("</w:tbl>")
    return "".join(out)


def docx_parts(body_xml, document_bytes=None):
    document = document_bytes if document_bytes is not None else (
        XML_DECL + f'<w:document xmlns:w="{NS_W}"><w:body>{body_xml}<w:sectPr/></w:body></w:document>').encode("utf-8")
    return [
        ("[Content_Types].xml", (XML_DECL + '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">'
         '<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>'
         '<Default Extension="xml" ContentType="application/xml"/>'
         '<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>'
         "</Types>").encode()),
        ("_rels/.rels", (XML_DECL + f'<Relationships xmlns="{NS_PKG}"><Relationship Id="rId1" Type="{REL}officeDocument" Target="word/document.xml"/></Relationships>').encode()),
        ("word/document.xml", document),
    ]


def write_docx(path, body_xml=None, document_bytes=None):
    write_zip(path, docx_parts(body_xml, document_bytes))


# ---------------------------------------------------------------- raw zip (for crafted/lying archives)
def raw_deflate(data, level=9):
    compressor = zlib.compressobj(level, zlib.DEFLATED, -15)
    return compressor.compress(data) + compressor.flush()


def write_raw_zip(path, entries):
    """entries: dicts with name, payload (already compressed), method, crc, csize, usize, flags."""
    body, directory = bytearray(), bytearray()
    for entry in entries:
        name = entry["name"].encode()
        offset = len(body)
        header = struct.pack("<IHHHHHIIIHH", 0x04034B50, 20, entry.get("flags", 0), entry["method"], 0, 0,
                             entry["crc"], entry["csize"], entry["usize"], len(name), 0)
        body += header + name + entry["payload"]
        directory += struct.pack("<IHHHHHHIIIHHHHHII", 0x02014B50, 20, 20, entry.get("flags", 0), entry["method"], 0, 0,
                                 entry["crc"], entry["csize"], entry["usize"], len(name), 0, 0, 0, 0, 0, offset) + name
    start = len(body)
    body += directory
    body += struct.pack("<IHHHHIIH", 0x06054B50, 0, 0, len(entries), len(entries), len(directory), start, 0)
    with open(path, "wb") as handle:
        handle.write(bytes(body))


def utf7_everything(text):
    """UTF-7 with every character inside one base64 run, so '<', '!' and '&' never appear literally."""
    raw = text.encode("utf-16-be")
    return b"+" + base64.b64encode(raw).rstrip(b"=") + b"-"

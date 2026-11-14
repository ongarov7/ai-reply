using System;
using System.Collections.Generic;
using System.Globalization;
using System.IO;
using System.Linq;
using System.Security.Cryptography;
using System.Text;

namespace AirReply.InstallerValidation
{
    // Reads resources as data, so an ARM64 payload can be checked on an x64 runner without loading/executing it.
    // PE resource layout: https://learn.microsoft.com/windows/win32/debug/pe-format#the-rsrc-section
    public static class EmbeddedIconVerifier
    {
        public static string AssertBrandIcon(string executablePath, string referenceIconPath)
        {
            var expected = ReadIco(File.ReadAllBytes(referenceIconPath));
            if (!expected.Any(f => f.Width == 16 && f.Height == 16) ||
                !expected.Any(f => f.Width == 256 && f.Height == 256))
                throw new InvalidDataException("The brand ICO must include small and high-DPI frames.");
            var image = new PeImage(File.ReadAllBytes(executablePath));
            var groups = image.Resources.Where(r => r.Type == "14").ToList();
            if (groups.Count == 0) throw new InvalidDataException("Executable has no RT_GROUP_ICON resources: " + executablePath);
            // Shell icon index zero is the first icon group. A hidden matching secondary icon is not sufficient.
            var primaryName = groups[0].Name;
            foreach (var group in groups.Where(g => g.Name == primaryName))
            {
                var actual = ReadGroup(group, image.Resources);
                if (actual.Count != expected.Count || expected.Any(e => !actual.Any(a => a.Width == e.Width &&
                    a.Height == e.Height && a.Hash == e.Hash)))
                    throw new InvalidDataException("Primary executable icon differs from the AI Reply brand ICO: " + executablePath);
            }
            return expected.Count.ToString(CultureInfo.InvariantCulture) + " exact brand frames; primary group " + primaryName;
        }

        private static List<Frame> ReadIco(byte[] bytes)
        {
            if (U16(bytes, 0) != 0 || U16(bytes, 2) != 1) throw new InvalidDataException("Expected an ICO file.");
            var count = U16(bytes, 4);
            if (count == 0 || count > 128) throw new InvalidDataException("Invalid ICO frame count.");
            var frames = new List<Frame>();
            for (var i = 0; i < count; i++)
            {
                var entry = 6 + i * 16;
                var size = checked((int)U32(bytes, entry + 8));
                var offset = checked((int)U32(bytes, entry + 12));
                frames.Add(new Frame(Dimension(bytes, entry), Dimension(bytes, entry + 1), Hash(Slice(bytes, offset, size))));
            }
            return frames;
        }

        private static List<Frame> ReadGroup(Resource group, List<Resource> resources)
        {
            var bytes = group.Bytes;
            if (U16(bytes, 0) != 0 || U16(bytes, 2) != 1) throw new InvalidDataException("Invalid group-icon resource.");
            var count = U16(bytes, 4);
            if (count == 0 || count > 128) throw new InvalidDataException("Invalid group-icon frame count.");
            var frames = new List<Frame>();
            for (var i = 0; i < count; i++)
            {
                var entry = 6 + i * 14;
                var size = checked((int)U32(bytes, entry + 8));
                var id = U16(bytes, entry + 12).ToString(CultureInfo.InvariantCulture);
                var matches = resources.Where(r => r.Type == "3" && r.Name == id).ToList();
                var icon = matches.FirstOrDefault(r => r.Language == group.Language) ?? matches.FirstOrDefault();
                if (icon == null || icon.Bytes.Length != size) throw new InvalidDataException("Group references a missing or truncated icon frame.");
                frames.Add(new Frame(Dimension(bytes, entry), Dimension(bytes, entry + 1), Hash(icon.Bytes)));
            }
            return frames;
        }

        private static int Dimension(byte[] bytes, int offset)
        {
            var value = Slice(bytes, offset, 1)[0];
            return value == 0 ? 256 : value;
        }
        private static string Hash(byte[] bytes)
        {
            using (var algorithm = SHA256.Create())
                return BitConverter.ToString(algorithm.ComputeHash(bytes)).Replace("-", "");
        }
        private static ushort U16(byte[] bytes, int offset) { Bounds(bytes, offset, 2); return BitConverter.ToUInt16(bytes, offset); }
        private static uint U32(byte[] bytes, int offset) { Bounds(bytes, offset, 4); return BitConverter.ToUInt32(bytes, offset); }
        private static byte[] Slice(byte[] bytes, int offset, int count)
        {
            Bounds(bytes, offset, count);
            var result = new byte[count];
            Buffer.BlockCopy(bytes, offset, result, 0, count);
            return result;
        }
        private static void Bounds(byte[] bytes, int offset, int count)
        {
            if (offset < 0 || count < 0 || offset > bytes.Length - count) throw new InvalidDataException("Truncated icon/PE resource.");
        }

        private sealed class Frame
        {
            internal readonly int Width, Height;
            internal readonly string Hash;
            internal Frame(int width, int height, string hash) { Width = width; Height = height; Hash = hash; }
        }
        private sealed class Resource
        {
            internal string Type = "", Name = "", Language = "";
            internal byte[] Bytes = new byte[0];
        }
        private sealed class Section
        {
            internal uint VirtualAddress, RawAddress, RawSize;
        }
        private sealed class PeImage
        {
            private readonly byte[] bytes;
            private readonly List<Section> sections = new List<Section>();
            private readonly int resourceBase;
            private readonly int resourceSize;
            internal readonly List<Resource> Resources = new List<Resource>();

            internal PeImage(byte[] bytes)
            {
                this.bytes = bytes;
                if (U16(bytes, 0) != 0x5a4d) throw new InvalidDataException("Expected Windows executable.");
                var pe = checked((int)U32(bytes, 0x3c));
                if (U32(bytes, pe) != 0x4550) throw new InvalidDataException("Invalid PE header.");
                var count = U16(bytes, pe + 6);
                var optionalSize = U16(bytes, pe + 20);
                var optional = pe + 24;
                var magic = U16(bytes, optional);
                var directories = magic == 0x10b ? optional + 96 : magic == 0x20b ? optional + 112 : 0;
                if (directories == 0) throw new InvalidDataException("Unsupported PE optional header.");
                Bounds(bytes, optional, optionalSize);
                if (directories + 24 > optional + optionalSize) throw new InvalidDataException("No PE resource directory.");
                for (var i = 0; i < count; i++)
                {
                    var entry = optional + optionalSize + 40 * i;
                    sections.Add(new Section { VirtualAddress = U32(bytes, entry + 12), RawSize = U32(bytes, entry + 16), RawAddress = U32(bytes, entry + 20) });
                }
                var rva = U32(bytes, directories + 16);
                resourceSize = checked((int)U32(bytes, directories + 20));
                resourceBase = Map(rva, resourceSize);
                Walk(0, new string[0]);
            }
            private int Map(uint rva, int length)
            {
                foreach (var section in sections)
                    if (rva >= section.VirtualAddress && (ulong)rva + (uint)length <= (ulong)section.VirtualAddress + section.RawSize)
                    {
                        var offset = checked((int)(section.RawAddress + (rva - section.VirtualAddress)));
                        Bounds(bytes, offset, length);
                        return offset;
                    }
                throw new InvalidDataException("PE resource RVA is not backed by file data.");
            }
            private int ResourceOffset(uint relative, int length)
            {
                if (relative > resourceSize || length > resourceSize - relative) throw new InvalidDataException("Invalid resource-directory offset.");
                return checked(resourceBase + (int)relative);
            }
            private string ResourceName(uint value)
            {
                if ((value & 0x80000000u) == 0) return value.ToString(CultureInfo.InvariantCulture);
                var relative = value & 0x7fffffffu;
                var count = U16(bytes, ResourceOffset(relative, 2));
                if (count > 1024) throw new InvalidDataException("Oversized resource name.");
                return Encoding.Unicode.GetString(bytes, ResourceOffset(relative + 2, count * 2), count * 2);
            }
            private void Walk(uint relative, string[] path)
            {
                if (path.Length > 2) throw new InvalidDataException("Unexpected resource-directory depth.");
                var offset = ResourceOffset(relative, 16);
                var count = U16(bytes, offset + 12) + U16(bytes, offset + 14);
                if (count > 4096) throw new InvalidDataException("Oversized resource directory.");
                ResourceOffset(relative + 16, count * 8);
                for (var i = 0; i < count; i++)
                {
                    var name = ResourceName(U32(bytes, offset + 16 + i * 8));
                    if (path.Length == 0 && name != "3" && name != "14") continue;
                    var address = U32(bytes, offset + 20 + i * 8);
                    var next = path.Concat(new[] { name }).ToArray();
                    if ((address & 0x80000000u) != 0) Walk(address & 0x7fffffffu, next);
                    else
                    {
                        if (next.Length != 3) throw new InvalidDataException("Unexpected resource leaf depth.");
                        var data = ResourceOffset(address, 16);
                        var size = checked((int)U32(bytes, data + 4));
                        if (size > 16 * 1024 * 1024) throw new InvalidDataException("Oversized icon resource.");
                        Resources.Add(new Resource { Type = next[0], Name = next[1], Language = next[2], Bytes = Slice(bytes, Map(U32(bytes, data), size), size) });
                    }
                }
            }
        }
    }
}

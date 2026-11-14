using System.Buffers.Binary;
using NAudio.Dsp;

namespace AirReply.Platform.Windows;

/// <summary>Streaming bounded mono PCM16 WAV. Independent of devices so limits and headers can be tested on any OS.</summary>
internal sealed class PcmWaveWriter : IDisposable
{
    private readonly Stream stream;
    private readonly long maximumSamples;
    private long samples;
    private bool finished;
    public PcmWaveWriter(Stream stream, TimeSpan maximumDuration)
    {
        this.stream = stream;
        maximumSamples = (long)(maximumDuration.TotalSeconds * 16000);
        stream.Write(new byte[44]);
    }
    public TimeSpan Duration => TimeSpan.FromSeconds(samples / 16000d);
    public bool AtLimit => samples >= maximumSamples;
    public float Append(ReadOnlySpan<short> values)
    {
        if (finished) return 0;
        var count = (int)Math.Min(values.Length, maximumSamples - samples);
        Span<byte> bytes = stackalloc byte[2];
        double sum = 0;
        for (var i = 0; i < count; i++)
        {
            BinaryPrimitives.WriteInt16LittleEndian(bytes, values[i]);
            stream.Write(bytes);
            var normalized = values[i] / 32768d;
            sum += normalized * normalized;
        }
        samples += count;
        return count == 0 ? 0 : Math.Clamp((float)Math.Sqrt(sum / count) * 3, 0, 1);
    }
    public void Dispose()
    {
        if (finished) return;
        finished = true;
        Span<byte> header = stackalloc byte[44];
        "RIFF"u8.CopyTo(header); "WAVEfmt "u8.CopyTo(header[8..]); "data"u8.CopyTo(header[36..]);
        BinaryPrimitives.WriteUInt32LittleEndian(header[4..], checked((uint)(samples * 2 + 36)));
        BinaryPrimitives.WriteUInt32LittleEndian(header[16..], 16);
        BinaryPrimitives.WriteUInt16LittleEndian(header[20..], 1);
        BinaryPrimitives.WriteUInt16LittleEndian(header[22..], 1);
        BinaryPrimitives.WriteUInt32LittleEndian(header[24..], 16000);
        BinaryPrimitives.WriteUInt32LittleEndian(header[28..], 32000);
        BinaryPrimitives.WriteUInt16LittleEndian(header[32..], 2);
        BinaryPrimitives.WriteUInt16LittleEndian(header[34..], 16);
        BinaryPrimitives.WriteUInt32LittleEndian(header[40..], checked((uint)(samples * 2)));
        stream.Position = 0;
        stream.Write(header);
        stream.Flush();
        stream.Dispose();
    }
}

/// <summary>Mix-format fallback: downmix all channels and preserve resampling phase across device packets.</summary>
internal sealed class Pcm16MonoConverter
{
    private readonly int sampleRate, channels, bits;
    private readonly bool floatingPoint;
    private readonly WdlResampler? resampler;
    public Pcm16MonoConverter(int sampleRate, int channels, int bits, bool floatingPoint)
    {
        if (sampleRate <= 0 || channels is < 1 or > 32 || bits is not (16 or 24 or 32) || floatingPoint && bits != 32)
            throw new InvalidDataException("Unsupported input format.");
        this.sampleRate = sampleRate; this.channels = channels; this.bits = bits; this.floatingPoint = floatingPoint;
        if (sampleRate != 16000)
        {
            resampler = new WdlResampler();
            resampler.SetMode(true, 2, false);
            resampler.SetFeedMode(true);
            resampler.SetRates(sampleRate, 16000);
        }
    }
    public short[] Convert(ReadOnlySpan<byte> bytes, bool silent)
    {
        var frameBytes = channels * (bits / 8);
        var frames = bytes.Length / frameBytes;
        var mono = new float[frames];
        for (var frame = 0; frame < frames; frame++)
        {
            float value = 0;
            if (!silent)
            {
                for (var channel = 0; channel < channels; channel++)
                {
                    var source = bytes[(frame * frameBytes + channel * (bits / 8))..];
                    value += (floatingPoint, bits) switch
                    {
                        (true, 32) => Math.Clamp(BinaryPrimitives.ReadSingleLittleEndian(source), -1, 1),
                        (false, 16) => BinaryPrimitives.ReadInt16LittleEndian(source) / 32768f,
                        (false, 24) => ((source[0] | source[1] << 8 | source[2] << 16) << 8 >> 8) / 8388608f,
                        (false, 32) => BinaryPrimitives.ReadInt32LittleEndian(source) / 2147483648f,
                        _ => throw new InvalidDataException("Unsupported input format."),
                    };
                }
                value /= channels;
            }
            mono[frame] = float.IsFinite(value) ? value : 0;
        }
        ReadOnlySpan<float> samples = mono;
        if (resampler != null && frames > 0)
        {
            var requested = resampler.ResamplePrepare(frames, 1, out var input);
            if (requested != frames) throw new InvalidDataException("Resampler rejected the device packet.");
            mono.AsSpan().CopyTo(input);
            var output = new float[(int)Math.Ceiling(frames * 16000d / sampleRate) + 128];
            var count = resampler.ResampleOut(output, frames, output.Length, 1);
            samples = output.AsSpan(0, count);
        }
        var result = new short[samples.Length];
        for (var i = 0; i < result.Length; i++)
            result[i] = (short)Math.Clamp((int)MathF.Round(samples[i] * 32768), short.MinValue, short.MaxValue);
        return result;
    }
}

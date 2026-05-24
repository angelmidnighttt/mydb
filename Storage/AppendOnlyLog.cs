using System.Text;

namespace MyDb.Storage;

public sealed class AppendOnlyLog : IDisposable
{
    private readonly FileStream _stream;
    private readonly object _writeLock = new();

    public string Path { get; }

    public AppendOnlyLog(string path)
    {
        Path = path;
        var dir = System.IO.Path.GetDirectoryName(path);
        if (!string.IsNullOrEmpty(dir)) Directory.CreateDirectory(dir);

        _stream = new FileStream(
            path,
            FileMode.OpenOrCreate,
            FileAccess.ReadWrite,
            FileShare.Read);
        _stream.Seek(0, SeekOrigin.End);
    }

    public long Append(string key, byte[] value, bool isTombstone = false)
    {
        ArgumentNullException.ThrowIfNull(key);
        ArgumentNullException.ThrowIfNull(value);

        var keyBytes = Encoding.UTF8.GetBytes(key);

        lock (_writeLock)
        {
            var offset = _stream.Position;

            Span<byte> header = stackalloc byte[9];
            BitConverter.TryWriteBytes(header[..4], keyBytes.Length);
            BitConverter.TryWriteBytes(header.Slice(4, 4), value.Length);
            header[8] = (byte)(isTombstone ? 1 : 0);

            _stream.Write(header);
            _stream.Write(keyBytes);
            _stream.Write(value);
            _stream.Flush(flushToDisk: true);

            return offset;
        }
    }

    public IEnumerable<LogRecord> ReadAll()
    {
        using var reader = new FileStream(
            Path,
            FileMode.Open,
            FileAccess.Read,
            FileShare.ReadWrite);

        var header = new byte[9];
        while (true)
        {
            var offset = reader.Position;
            var read = reader.Read(header, 0, header.Length);
            if (read == 0) yield break;
            if (read < header.Length) yield break;

            var keyLen = BitConverter.ToInt32(header, 0);
            var valueLen = BitConverter.ToInt32(header, 4);
            var isTombstone = header[8] == 1;

            if (keyLen < 0 || valueLen < 0) yield break;

            var keyBytes = new byte[keyLen];
            if (reader.Read(keyBytes, 0, keyLen) != keyLen) yield break;

            var valueBytes = new byte[valueLen];
            if (valueLen > 0 && reader.Read(valueBytes, 0, valueLen) != valueLen) yield break;

            yield return new LogRecord
            {
                Key = Encoding.UTF8.GetString(keyBytes),
                Value = valueBytes,
                IsTombstone = isTombstone,
                Offset = offset,
            };
        }
    }

    public void Dispose() => _stream.Dispose();
}

namespace MyDb.Storage;

public sealed class LogRecord
{
    public required string Key { get; init; }
    public required byte[] Value { get; init; }
    public bool IsTombstone { get; init; }
    public long Offset { get; init; }
}

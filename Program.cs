using System.Text;
using MyDb.Storage;

var dataDir = Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "data");
var logPath = Path.GetFullPath(Path.Combine(dataDir, "mydb.log"));
Console.WriteLine($"Log file: {logPath}");

using (var log = new AppendOnlyLog(logPath))
{
    log.Append("user:1", Encoding.UTF8.GetBytes("Alice"));
    log.Append("user:2", Encoding.UTF8.GetBytes("Bob"));
    log.Append("user:1", Encoding.UTF8.GetBytes("Alice v2"));
    log.Append("user:2", [], isTombstone: true);
}

using (var log = new AppendOnlyLog(logPath))
{
    Console.WriteLine("--- Replay ---");
    foreach (var record in log.ReadAll())
    {
        var value = Encoding.UTF8.GetString(record.Value);
        var marker = record.IsTombstone ? "[DEL]" : "[PUT]";
        Console.WriteLine($"{marker} offset={record.Offset} key={record.Key} value={value}");
    }
}

# 04 — Write-ahead log

Package: [`internal/wal`](../internal/wal/log.go) (phần `Log`) và [`internal/kv`](../internal/kv/kv.go)

## Mục tiêu

Đến bước [02](02-in-memory-store.md), tắt chương trình là mất sạch dữ liệu. Bước này sửa
điều đó: mỗi thay đổi được **ghi xuống đĩa trước**, rồi mới áp vào memory. Khởi động lại
thì đọc lại file đó từ đầu để dựng lại đúng trạng thái cũ.

Đó là ý nghĩa của cái tên **write-ahead log**: log đi *trước* thay đổi trong bộ nhớ.

## Vì sao là append-only

File log chỉ có một thao tác duy nhất: nối thêm vào cuối. Không sửa tại chỗ, không xóa
giữa file. Ba lý do:

1. **Nhanh.** Ghi nối đuôi là tuần tự, đĩa không phải nhảy qua nhảy lại tìm chỗ. Kể cả
   SSD cũng thích ghi tuần tự hơn.
2. **Hỏng thì hỏng ở cuối.** Mất điện giữa lúc ghi chỉ làm hỏng record cuối cùng, những
   record trước đó đã nằm yên và không bị đụng tới. Nếu sửa tại chỗ, một lần ghi dở dang
   có thể phá hỏng dữ liệu vốn đang tốt.
3. **Lịch sử được giữ nguyên.** Log là chuỗi các thay đổi theo thứ tự thời gian, không
   phải ảnh chụp trạng thái cuối. Đây là nền cho replication và point-in-time recovery
   về sau.

Cái giá phải trả: file phình mãi, và key ghi đè 1000 lần thì có 1000 record trong khi chỉ
1 record còn giá trị. Xử lý chuyện đó gọi là compaction — chưa làm.

## `Log`

```go
type Log struct {
	FileName string
	fp       *os.File
}

func (l *Log) Open() error
func (l *Log) Close() error
func (l *Log) Write(ent *Entry) error
func (l *Log) Read(ent *Entry) (eof bool, err error)
func (l *Log) Sync() error
```

`Read` bọc `Entry.Decode` và dịch `io.EOF` thành `eof = true` — kết thúc bình thường, để
vòng lặp replay dừng lại. Mọi lỗi khác được trả nguyên: một record đứt giữa chừng
(`io.ErrUnexpectedEOF`) là log hỏng, **không phải** hết file.

### Một con trỏ dùng chung cho cả đọc và ghi

File mở bằng `os.O_RDWR|os.O_CREATE` — đọc ghi chung, và **chung một con trỏ vị trí**.
Sau khi replay chạy tới hết file, con trỏ nằm ở cuối; đó chính là lý do `Write` sau đó
nối thêm vào đuôi thay vì đè lên đầu file.

Hệ quả là **phải replay hết trước khi ghi**. Nếu dừng replay giữa chừng rồi gọi `Write`,
record mới sẽ đè lên dữ liệu cũ và phá hỏng log. `TestLogWriteAppendsAfterReplay` và
`TestWritesAfterRestartAppend` canh chỗ này.

Cùng lý do đó, đừng vội bọc `bufio.Reader` quanh file để replay nhanh hơn: `bufio` đọc dư
vào bộ đệm, con trỏ thật của file sẽ nhảy quá chỗ ta đã dùng, và lần `Write` kế tiếp ghi
sai vị trí. Muốn dùng đệm thì phải `Seek` lại đúng chỗ, hoặc mở riêng một file handle cho
việc ghi với cờ `O_APPEND`.

## `KV`

```go
type KV struct {
	Path string

	mu  sync.Mutex
	log wal.Log
	mem *store.Store
}
```

`mem` là `store.Store` từ [02](02-in-memory-store.md) — không viết lại `map` + khóa lần
nữa. Toàn bộ thao tác đọc phục vụ từ RAM; log chỉ để đọc lúc khởi động và ghi lúc thay đổi.

### Open: replay

```go
func (kv *KV) Open() error
```

Mở file, tạo store rỗng, rồi lặp `log.Read` tới `eof`, áp từng record vào memory theo
đúng thứ tự trong file. Nhờ đọc theo thứ tự ghi mà **record sau tự động đè record trước** —
không cần so sánh timestamp hay version gì cả, thứ tự trong file *chính là* thứ tự thời gian.

Nếu replay lỗi, `Open` đóng file và trả lỗi. Không mở nửa vời: một database lên với dữ
liệu thiếu mà không báo gì còn tệ hơn là không lên được.

### Set và Del

```go
func (kv *KV) Set(key []byte, val []byte) (updated bool, err error)
func (kv *KV) Del(key []byte) (deleted bool, err error)
```

Cả hai theo đúng một trình tự: **ghi log trước, sửa memory sau**.

```go
if err := kv.log.Write(&ent); err != nil {
	return false, err      // memory chưa bị đụng tới
}
kv.apply(&ent)
```

Nếu đảo ngược thứ tự — sửa memory trước rồi ghi log — thì khi ghi log lỗi, memory đã thay
đổi còn đĩa thì không. Database đang chạy trả về một đằng, khởi động lại ra một nẻo. Ghi
log trước thì lỗi đồng nghĩa với "không có gì xảy ra cả", đó là trạng thái dễ hiểu và dễ
xử lý hơn nhiều.

`updated` cho biết key đã tồn tại từ trước (ghi đè) hay là mới (chèn). `deleted` cho biết
key có thật để mà xóa. Xóa một key không tồn tại thì **không ghi gì cả** — tombstone cho
một key chưa từng được set không mang thông tin gì, chỉ làm log to thêm.

### Khóa

`kv.mu` giữ cho thứ tự record trong log khớp với thứ tự thay đổi trong memory. Nếu hai
goroutine cùng ghi mà không có khóa này, chúng có thể ghi log theo thứ tự A→B nhưng áp
vào memory theo thứ tự B→A; lúc đó trạng thái đang chạy và trạng thái sau khi replay sẽ
khác nhau — một loại bug chỉ lộ ra sau khi restart.

Đọc (`Get`) không đi qua `kv.mu`, mà dùng thẳng `RWMutex` bên trong `store` — nhiều
goroutine đọc song song vẫn thoải mái.

## Giới hạn hiện tại

- **Log cắt cụt làm chết hẳn `Open`.** Mất điện giữa lúc ghi để lại một record dở ở cuối
  file, lần khởi động sau `Open` trả lỗi và database không lên được nữa. Đây là lỗ hổng
  nghiêm trọng nhất hiện tại: sự cố *chắc chắn* sẽ xảy ra, mà cách xử lý lại là chết hẳn.
  Cách làm đúng là nhận ra record dở ở cuối, cắt file về ranh giới record tốt cuối cùng
  rồi chạy tiếp — chỉ an toàn khi đã có checksum để phân biệt "dở dang" với "hỏng thật".
- **Không tự `Sync`.** `Write` chỉ đưa byte tới hệ điều hành, chưa chắc đã xuống đĩa vật
  lý. Process chết thì dữ liệu còn, nhưng mất điện thì mất vài thay đổi cuối. Có sẵn
  `Sync()` để gọi thủ công; gọi sau mỗi lần ghi thì an toàn nhất nhưng chậm đi hàng chục
  lần. Chọn thế nào là đánh đổi, sẽ tính khi có server.
- **Log không bao giờ nhỏ lại.** Chưa có compaction. Ghi đè cùng một key mãi thì file cứ
  lớn dần, và thời gian khởi động lớn theo kích thước file chứ không theo số key.
- **Toàn bộ dữ liệu phải vừa trong RAM.** Log chỉ để khôi phục, mọi thao tác đọc vẫn từ
  memory.
- **Một tiến trình duy nhất.** Không có file lock; hai tiến trình cùng mở một file log sẽ
  ghi đè lẫn nhau và làm hỏng log.

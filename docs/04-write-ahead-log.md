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
func (l *Log) Write(ent *Entry) error   // ghi xong fsync rồi mới trả về
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

## Đảm bảo dữ liệu thật sự xuống đĩa

### Durability nghĩa là gì

**Durability** là lời hứa: dữ liệu đã ghi thì không mất. Nhưng lời hứa đó phải gắn với một
mốc cụ thể, và mốc đó là **lúc hàm trả về thành công**.

Nếu database chết *trước khi* trả về, người gọi không biết thao tác đã thành hay chưa —
trạng thái không xác định, và điều đó chấp nhận được. Nhưng một khi `Set` đã trả `nil`,
người gọi phải tin được rằng dù có rút điện ngay giây sau, dữ liệu vẫn còn đó.

### Vì sao `Write` thôi là chưa đủ

`fp.Write()` không đẩy byte xuống đĩa. Nó chỉ chép vào **page cache** của hệ điều hành,
rồi trả về ngay. OS ghi xuống đĩa sau, theo lịch của nó.

Cache này gọi là page cache vì đơn vị của nó là *page* — cùng khái niệm page với bộ nhớ ảo
của CPU, kích thước cố định thường 4K hoặc 16K, và cũng là đơn vị IO nhỏ nhất. Đây là lý
do nó tồn tại: gộp nhiều lần ghi nhỏ vào cùng một page thành một lần ghi đĩa, và giữ lại
dữ liệu vừa đọc để lần sau khỏi đọc lại. (Muốn hiểu vì sao cache IO lại dính tới bộ nhớ
ảo thì tìm hiểu `mmap`. Lưu ý tài liệu database cũng gọi node của B-tree là "page" — hai
khái niệm khác nhau, đừng nhầm.)

Chưa hết: bản thân ổ đĩa cũng có RAM cache riêng. Byte đã rời khỏi page cache vẫn có thể
đang nằm trong cache của đĩa chứ chưa ghi lên vật lý.

Muốn chắc chắn thì phải xả **hết mọi tầng cache và đợi xong**. Đó là syscall `fsync` trên
Linux, trong Go là `Sync()` của `*os.File`. Windows có thao tác tương ứng nên `Sync()` cũng
dùng được.

```go
func (l *Log) Write(ent *Entry) error {
	if _, err := l.fp.Write(ent.Encode()); err != nil {
		return err
	}
	return l.fp.Sync() // fsync
}
```

Nói thêm: một số thư viện IO còn có cache riêng ở tầng ứng dụng, phải xả trước khi fsync —
`fflush()` trong C là ví dụ. Go thì không, IO của Go ánh xạ thẳng xuống API của OS.

### fsync cả thư mục cha

Trên Unix có một cái bẫy: `fsync` trên file đảm bảo **nội dung** file đã xuống đĩa, nhưng
không đảm bảo **bản thân file tồn tại**.

Lý do: tên file không nằm trong file, nó nằm trong thư mục cha — một object riêng, có
trang dirty riêng. Tạo file xong, ghi dữ liệu xong, fsync xong, nhưng mất điện trước khi
thư mục kịp xuống đĩa: dữ liệu nằm trên đĩa mà không có cái tên nào trỏ tới nó.

Vậy nên **tạo file, đổi tên file, xóa file** đều cần fsync thư mục chứa nó. Đây là chuyện
của Unix; Windows không cần, và cũng không cho phép mở thư mục ra để sync như vậy.

Thư viện chuẩn Go không có sẵn hàm fsync thư mục, nhưng `os.Open` mở được thư mục ở chế độ
chỉ đọc và `Sync()` trên nó chính là `fsync` trên file descriptor của thư mục:

```go
//go:build unix

func syncDir(name string) error {
	dir, err := os.Open(filepath.Dir(name))
	if err != nil {
		return err
	}
	defer dir.Close()

	return dir.Sync()
}
```

Bản cho Windows nằm ở file riêng với cờ `//go:build !unix` và không làm gì cả. Tách theo
build tag là cách Go xử lý khác biệt hệ điều hành: trình biên dịch chỉ nạp file khớp với
nền tảng đang build, nên không cần `if runtime.GOOS == ...` rải trong code.

> **File descriptor.** `open` trả về một con số định danh cho file vừa mở, dùng cho mọi
> thao tác sau đó — kể cả `fsync`. Con số đó gọi là file descriptor (fd). Nó không chỉ đại
> diện cho file: socket mạng, thư mục, pipe đều là fd. Trên Unix mở được cả thư mục, chỉ
> là fd đó không đọc/ghi như file thường. Mọi fd đều phải được đóng. `os.File` trong thư
> viện chuẩn về cơ bản là lớp bọc quanh các syscall này.

### Cái giá của fsync

Đo trên máy dev (`BenchmarkWrite` và `BenchmarkWriteNoSync` chỉ khác nhau đúng lời gọi fsync):

| | mỗi lần ghi | throughput |
|---|---|---|
| `Write` + fsync | ~346 µs | ~2.900 ghi/giây |
| `Write` không fsync | ~4 µs | ~246.000 ghi/giây |

**Chậm hơn 85 lần.** Đó không phải lãng phí — đó là cái giá thật của việc đợi phần cứng
xác nhận. Nhưng nó cho thấy vì sao mọi database đều cho phép chỉnh mức độ này: Redis có
`appendfsync always|everysec|no`, Postgres có `synchronous_commit`. Đánh đổi nằm ở chỗ
chấp nhận mất bao nhiêu giây dữ liệu cuối cùng khi mất điện.

Hiện tại mydb chọn mức an toàn nhất: fsync sau **mỗi** record.

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
- **fsync mỗi record nên ghi rất chậm** — khoảng 2.900 ghi/giây, so với 246.000 nếu không
  fsync. Chưa có chế độ gộp nhiều record vào một lần fsync, cũng chưa có tùy chọn cho phép
  người dùng chấp nhận mất vài giây dữ liệu để đổi lấy tốc độ.
- **Đường fsync thư mục chưa chạy thật lần nào.** Máy dev là Windows nên `syncDir` luôn
  là hàm rỗng; bản Unix mới chỉ được kiểm tra bằng cross-compile (`GOOS=linux go vet`),
  chưa chạy trên Linux thật.
- **Log không bao giờ nhỏ lại.** Chưa có compaction. Ghi đè cùng một key mãi thì file cứ
  lớn dần, và thời gian khởi động lớn theo kích thước file chứ không theo số key.
- **Toàn bộ dữ liệu phải vừa trong RAM.** Log chỉ để khôi phục, mọi thao tác đọc vẫn từ
  memory.
- **Một tiến trình duy nhất.** Không có file lock; hai tiến trình cùng mở một file log sẽ
  ghi đè lẫn nhau và làm hỏng log.

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

## Ghi dở: torn write

fsync giải quyết chuyện "dữ liệu có xuống đĩa không". Còn một câu hỏi nữa: khi ghi một
record 1000 byte mà mất điện giữa chừng thì file trông như thế nào?

Ta muốn record hoặc **được ghi trọn vẹn, hoặc không có gì cả** — tính chất đó gọi là
**atomicity**. Nhưng thao tác ghi file **không hề đảm bảo** điều đó khi mất điện. Không
chỉ dữ liệu ghi thiếu, mà ngay cả kích thước file cũng có thể sai. Nối thêm 1000 byte có
thể cho ra:

- file tăng đúng 1000 byte, nhưng dữ liệu chưa ghi hết;
- file tăng đúng 1000 byte, không có dữ liệu nào được ghi, chỗ trống toàn `0x00` hoặc rác;
- file chỉ tăng 500 byte.

Hiện tượng này gọi là **torn write** (ghi rách). Điểm mấu chốt: **chỉ record cuối cùng bị
ảnh hưởng**, mọi record đã fsync thành công trước đó vẫn nguyên vẹn. Đây là thêm một lý do
nữa để database dùng log — hỏng thì hỏng ở đuôi, phần thân không bị đụng tới.

### Atomicity ở tầng phần cứng

CPU có các lệnh đọc/ghi bộ nhớ atomic cho dữ liệu cỡ một số nguyên. Nhưng đó là atomic
cho **tương tranh**; ở đây ta cần atomic cho **lưu trữ khi mất điện** — hai chuyện khác nhau.

Ở tầng phần cứng, ghi trọn **một sector** thường là atomic. Sector là đơn vị đọc/ghi nhỏ
nhất của đĩa, thường 512B hoặc 4K.

Nhiều hệ thống dựa vào tính atomic cỡ sector đó để xây atomicity ở quy mô lớn hơn. Ví dụ:
dành riêng một sector ở đầu log để lưu vị trí của record cuối cùng hợp lệ; ghi log xong,
fsync xong, mới cập nhật sector đó (và fsync tiếp). Cách này cho atomicity cho toàn bộ
log, nhưng tốn **hai lần fsync** mỗi lần ghi — mà fsync là thứ đắt nhất.

Có cách khác không cần đảm bảo gì từ phần cứng, cũng không cần hai lần fsync.

### Checksum: phát hiện ghi dở

Giả sử ghi log không atomic. Nhưng nếu **phát hiện được** record ghi dở thì chỉ cần bỏ qua
nó là xong. Record duy nhất bị ảnh hưởng là cái nằm sau lần fsync thành công cuối cùng.

Checksum làm được việc đó. Nó là một hàm băm: dữ liệu khác nhau thì gần như chắc chắn cho
giá trị khác nhau. Lưu checksum kèm mỗi record, lúc đọc tính lại rồi đối chiếu — không
khớp nghĩa là record không toàn vẹn.

mydb dùng `crc32.ChecksumIEEE()` của thư viện chuẩn, đặt ở **đầu** record và phủ toàn bộ
phần còn lại:

```
|  crc32  | key size | val size | deleted | key data | val data |
| 4 bytes | 4 bytes  | 4 bytes  | 1 byte  |   ...    |   ...    |
```

`TestDecodeDetectsFlippedBit` lật thử một bit ở **từng byte** của record và đòi hỏi lần
nào cũng bị từ chối.

Vì sao dùng crc32 mà không phải sha256 hay md5? Hàm băm mật mã cũng làm được, nhưng không
có lý do gì để dùng: hàm checksum chuyên dụng cho kết quả ngắn hơn và chạy nhanh hơn nhiều.
Kể cả cách đơn giản như tổng số nguyên 16-bit của TCP/IP cũng đủ dùng — chỉ cần lưu ý
trường hợp toàn byte 0 thì checksum không được ra 0, và crc32 không dính vấn đề đó.

### Xử lý record dở lúc replay

`Log.Read` phân loại lỗi từ `Entry.Decode` rồi quyết định:

| Lỗi | Xử lý | Vì sao |
|---|---|---|
| `io.EOF` | `eof = true` | hết log bình thường |
| `io.ErrUnexpectedEOF` | `eof = true`, cắt đuôi | record ghi dở |
| `ErrBadSum` | `eof = true`, cắt đuôi | record không toàn vẹn |
| `ErrCorruptEntry` | trả lỗi | dữ liệu nguyên vẹn nhưng vô nghĩa — không phải ghi dở |

Bỏ qua record dở là **an toàn** chứ không phải làm ngơ: lần ghi đó chưa bao giờ trả về
thành công cho người gọi, nên theo định nghĩa durability ở trên, ta chưa hứa gì về nó cả.
Ngược lại, mọi record đã trả về thành công đều đã fsync xong và nằm trước nó.

### Bắt buộc phải cắt đuôi file

Phát hiện record dở thôi chưa đủ — phải **xóa nó khỏi file**, và đây là chỗ dễ bỏ sót nhất.

Sau lần đọc thất bại, con trỏ file đang nằm giữa đống rác. Nếu cứ thế mà `Write`, record
mới sẽ nằm *sau* đống rác đó. Lần khởi động kế tiếp: replay chạy tới đống rác, thấy hỏng,
coi như hết log — và **mất trắng mọi record ghi sau lần khôi phục**, lặng lẽ, không báo gì.

Nên `Log.Read` cắt file về đúng cuối record hợp lệ cuối cùng rồi mới báo eof:

```go
func (l *Log) truncateTail() error {
	if err := l.fp.Truncate(l.offset); err != nil {
		return err
	}
	if _, err := l.fp.Seek(l.offset, io.SeekStart); err != nil {
		return err
	}
	return l.fp.Sync()
}
```

`TestLogWriteAfterTornTailRecovery` và `TestOpenRecoversFromTornWrite` canh đúng kịch bản
này: hỏng đuôi → khôi phục → ghi tiếp → khởi động lại → dữ liệu mới vẫn còn.

### Cái checksum không làm được

Checksum cũng bắt được lỗi phần cứng, kiểu bit bị lật do đĩa hoặc RAM lỗi. Nhưng đó
**không phải** mục đích của nó trong database, vì database không khôi phục được loại mất
mát đó — nó chỉ báo cho ta biết dữ liệu đã hỏng.

Và có một câu hỏi không có lời đáp: làm sao biết record hỏng có phải là record cuối cùng
hay không? **Không có cách nào**, vì trường size của chính record đó cũng có thể sai, nên
không thể biết record kế tiếp bắt đầu ở đâu. Hệ quả: một record hỏng ở **giữa** log làm
mất luôn mọi record đứng sau nó. `TestOpenStopsAtDamageInTheMiddle` ghi nhận đúng hành vi đó.

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

## Tóm lại

Ba mảnh ghép — **log + checksum + fsync** — cho database khôi phục được sau khi mất điện
và không đánh mất những lần ghi đã báo thành công. Đó là chức năng cốt lõi của một database.

Đây cũng là lý do người ta nhúng SQLite vào ứng dụng di động thay vì ghi thẳng JSON ra
file: không chỉ để truy vấn cho tiện, mà vì thao tác file thông thường **không** đảm bảo
được durability và atomicity.

Hiện mydb mới chỉ có log. Khi thêm cấu trúc dữ liệu vào, atomicity và durability — chữ A
và D của ACID — sẽ phải tính lại từ đầu.

## Giới hạn hiện tại

- **Hỏng ở giữa log làm mất mọi thứ phía sau.** Không tránh được với format hiện tại, như
  đã nói ở trên. Muốn khá hơn thì cần chia log thành block cố định để tìm lại được ranh
  giới record kế tiếp — cách LevelDB và Kafka làm.
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

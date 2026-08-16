# 03 — Serialization

Package: [`internal/wal`](../internal/wal/entry.go)

## Mục tiêu

Cấu trúc dữ liệu trong Go — struct, slice, map — chỉ tồn tại trong bộ nhớ của tiến trình.
Đĩa và network không hiểu chúng, hai thứ đó chỉ nhận **một chuỗi byte**. Chuyển struct
thành chuỗi byte gọi là **serialization**, chiều ngược lại là **deserialization**.

Bước này định nghĩa cách một cặp key-value biến thành byte và ngược lại. Đây là định dạng
record mà write-ahead log ở bước sau sẽ ghi xuống file.

## Định dạng

```
|  crc32  | key size | val size | deleted | key data | val data |
| 4 bytes | 4 bytes  | 4 bytes  | 1 byte  |   ...    |   ...    |
```

`key="a"`, `val="bb"` cho ra:

```
[59 37 55 31 | 1 0 0 0 | 2 0 0 0 | 0 | 97 | 98 98]
 └────┬────┘   └──┬──┘   └──┬──┘  ─┬─  ─┬─  └─┬─┘
   crc32       len=1     len=2  deleted 'a'  "bb"
      └──────────── checksum phủ toàn bộ phần này ────┘
```

Checksum phủ **mọi thứ đứng sau nó** — cả header lẫn dữ liệu. Chi tiết vì sao cần nó nằm
ở [04](04-write-ahead-log.md#checksum-phát-hiện-ghi-dở).

### Vì sao cần cờ `deleted`

Log chỉ có một thao tác: nối thêm vào cuối. Không sửa, không xóa giữa file. Vậy làm sao
ghi lại việc "xóa key này"? Bằng cách nối thêm một record nói rằng key đó đã bị xóa —
gọi là **tombstone** (bia mộ).

Khi replay, gặp record thường thì ghi vào memory, gặp tombstone thì xóa khỏi memory. Vì
đọc theo đúng thứ tự ghi nên trạng thái cuối cùng luôn phản ánh thao tác cuối cùng.

Nếu không có cờ này, xóa một key sẽ không để lại dấu vết nào trong log, và key đó sống
lại sau mỗi lần khởi động — `TestDeleteSurvivesRestart` trong package `kv` canh đúng chỗ đó.

Cờ chỉ nhận giá trị 0 hoặc 1. Giá trị khác nghĩa là những byte này không phải do ta ghi
ra, `Decode` trả về `ErrCorruptEntry` thay vì đoán bừa — đọc tiếp sẽ diễn giải sai toàn
bộ phần còn lại của file.

### Vì sao size phải đứng trước

Với kiểu có độ dài thay đổi (slice, string), người đọc cần biết **đọc bao nhiêu byte**
trước khi đọc. Nếu ghi dữ liệu trước rồi mới ghi độ dài, người đọc phải đọc xong mới biết
mình lẽ ra cần đọc bao nhiêu — vô nghĩa.

Cách khác là dùng ký tự phân cách (kiểu CSV), nhưng khi đó dữ liệu chứa đúng ký tự phân
cách sẽ phá vỡ định dạng, và phải escape. Value ở đây là byte tùy ý — mọi byte đều có thể
xuất hiện — nên **length-prefix** là lựa chọn đúng: không có byte nào mang ý nghĩa đặc biệt.

### Vì sao là little-endian uint32

Số nguyên nhiều byte có thể xếp theo hai thứ tự: byte thấp trước (little-endian) hay byte
cao trước (big-endian). Chọn cái nào cũng được, nhưng **phải cố định** — file ghi ở máy
này đọc ở máy khác phải ra cùng kết quả. `binary.LittleEndian` ghi rõ ràng thứ tự đó
trong code, không phụ thuộc kiến trúc CPU đang chạy.

Little-endian vì phần lớn CPU phổ biến (x86-64, ARM) vốn dùng nó, khỏi phải hoán đổi byte.
`uint32` cho phép key/value tới 4 GiB — thừa sức, mà chỉ tốn 4 byte header mỗi bên.

## Encode

```go
func (ent *Entry) Encode() []byte {
	data := make([]byte, headerSize+len(ent.Key)+len(ent.Val))

	body := data[sumSize:] // phần được checksum phủ

	binary.LittleEndian.PutUint32(body[0:4], uint32(len(ent.Key)))
	binary.LittleEndian.PutUint32(body[4:8], uint32(len(ent.Val)))
	if ent.Deleted {
		body[8] = flagDeleted
	}
	copy(body[metaSize:], ent.Key)
	copy(body[metaSize+len(ent.Key):], ent.Val)

	binary.LittleEndian.PutUint32(data[0:sumSize], crc32.ChecksumIEEE(body))

	return data
}
```

Tính đúng tổng kích thước rồi cấp phát **một lần duy nhất** — benchmark cho thấy 1 alloc
mỗi lần gọi. Nếu dùng `append` dần thì slice phải grow nhiều lần, mỗi lần là một lần cấp
phát và copy lại toàn bộ.

## Decode và `io.Reader`

```go
func (ent *Entry) Decode(r io.Reader) error
```

Người gọi không biết trước entry dài bao nhiêu byte, nên không thể truyền vào một slice
có sẵn. Thay vào đó truyền một **nguồn để đọc dần**: `io.Reader`.

```go
type Reader interface {
	Read(p []byte) (n int, err error)
}
```

Bất cứ kiểu nào có method `Read` đều dùng được. Nhờ vậy `Decode` không cần biết dữ liệu
đến từ đâu: test đọc từ `bytes.Buffer` trong RAM, bước sau đọc từ `*os.File`, sau nữa có
thể là network connection — cùng một hàm, không sửa dòng nào. Chiều ngược lại là
`io.Writer` với method `Write`.

Đây chính là ý tưởng của syscall `read`/`write` trong Unix: file, socket, pipe, IPC đều
rất khác nhau, nhưng điểm chung là đọc và ghi, nên hệ điều hành cho chúng chung một giao diện.

### Cái bẫy: `Read` được phép trả về thiếu

Điểm dễ sai nhất của toàn bộ phần này:

> `r.Read(p)` **không** hứa lấp đầy `p`. Nó được phép trả về ít byte hơn ta xin mà không
> hề báo lỗi.

Với `bytes.Buffer` thì hầu như lần nào cũng đủ, nên code sai vẫn qua test. Nhưng đọc từ
file hay socket thì trả về thiếu là chuyện bình thường — gói tin chưa về hết, đọc chạm
biên buffer. Gọi `Read` một lần rồi coi như xong sẽ sinh ra lỗi chỉ xuất hiện lúc chạy
thật, dưới tải, và cực khó tái hiện.

Cách đúng là `io.ReadFull`, nó lặp cho tới khi đủ:

```go
var header [headerSize]byte
if _, err := io.ReadFull(r, header[:]); err != nil {
	return err
}
```

`TestDecodeShortReads` dùng `iotest.OneByteReader` — một reader cố tình mỗi lần chỉ trả
đúng 1 byte — để ép lỗi này lộ ra ngay trên máy dev thay vì ngoài production.

### Bốn loại kết thúc

Phân biệt được các trường hợp này là điều kiện để replay log an toàn — mỗi loại dẫn tới
một cách xử lý khác nhau ở [04](04-write-ahead-log.md):

| Tình huống | Trả về | Ý nghĩa |
|---|---|---|
| Hết stream đúng ranh giới record | `io.EOF` | bình thường — đã đọc xong, dừng vòng lặp |
| Hết stream giữa chừng một record | `io.ErrUnexpectedEOF` | record bị cắt cụt, ghi chưa xong |
| Đủ byte nhưng checksum sai | `ErrBadSum` | dữ liệu không toàn vẹn |
| Checksum đúng nhưng trường vô nghĩa | `ErrCorruptEntry` | không phải hỏng — là bug hoặc lệch format |

`io.ReadFull` trả `io.EOF` khi chưa đọc được byte nào, và `io.ErrUnexpectedEOF` khi đọc
được một phần. Nhưng có một khe hở: nếu stream kết thúc **đúng ngay sau header**, lời gọi
đọc key bắt đầu từ 0 byte nên `ReadFull` trả `io.EOF` — trong khi thực chất đây là record
bị cắt cụt, vì header đã hứa có key. Hàm `readBody` nâng `io.EOF` thành
`io.ErrUnexpectedEOF` đúng cho trường hợp đó.

`TestDecodeTruncated` kiểm tra các vị trí cắt: giữa header, ngay sau header, giữa key,
giữa val.

### Kiểm checksum trước, đọc cờ sau

Thứ tự này có chủ ý. Cờ `deleted` nằm trong header, nhưng **chừng nào checksum chưa khớp
thì không có byte nào trong header đáng tin** — kể cả cái cờ đó. Nên `Decode` đọc đủ dữ
liệu, đối chiếu checksum, rồi mới diễn giải cờ.

Nhờ vậy hai lỗi tách bạch được: `ErrBadSum` là "dữ liệu hỏng", còn `ErrCorruptEntry` là
"dữ liệu nguyên vẹn nhưng chứa giá trị package này không bao giờ ghi ra" — tức bug hoặc
đọc nhầm format của phiên bản khác. Hai thứ đó cần xử lý khác nhau: cái đầu bỏ qua được,
cái sau thì không.

### Không tin `size` khi chưa kiểm checksum

`size` đọc từ header mà header thì chưa được kiểm chứng. Một record ghi dở có thể để lại
bất kỳ thứ gì trong bốn byte đó — kể cả `0xFFFFFFFF`, tức 4 GiB.

Nếu cứ `make([]byte, size)` theo con số ấy, một file hỏng vài chục byte sẽ khiến chương
trình cố cấp phát 4 GiB và chết vì hết bộ nhớ — **trước khi** checksum kịp nói rằng record
đó vô giá trị. Đúng cái tình huống mà checksum sinh ra để xử lý lại thành thứ giết chương
trình.

Nên `readBody` chỉ cấp phát trước với record nhỏ (dưới 1 MiB); lớn hơn thì cho buffer lớn
dần theo lượng byte thật sự đọc được. Record 4 GiB giả mạo trong file 100 byte chỉ cấp
phát khoảng 100 byte rồi trả `io.ErrUnexpectedEOF`. `TestDecodeHugeSizeIsNotAllocated`
canh chỗ này.

### Replay

Entry nằm sát nhau, không có dấu phân cách, nên đọc log là một vòng lặp tới `io.EOF`:

```go
for {
	var ent wal.Entry
	err := ent.Decode(r)
	if errors.Is(err, io.EOF) {
		break            // hết log, bình thường
	}
	if err != nil {
		return err       // log hỏng — phải xử lý, không được bỏ qua
	}
	apply(ent)
}
```

`Decode` cấp phát slice mới cho `Key` và `Val` mỗi lần gọi, nên entry đọc trước không bị
lần gọi sau ghi đè — cùng lý do với chuyện copy value ở [02](02-in-memory-store.md).
`TestDecodeDoesNotAlias` giữ điều này.

## Giới hạn hiện tại

- **Checksum chỉ phát hiện, không sửa được.** Nó nói cho ta biết record hỏng, chứ không
  khôi phục được nội dung. Muốn sửa thì cần mã sửa lỗi hoặc một bản sao khác.
- **Không có số hiệu phiên bản format.** Đọc file của phiên bản sau sẽ báo lỗi khó hiểu
  thay vì nói thẳng "format này mới hơn".
- Key/val giới hạn 4 GiB do `uint32`. Không phải vấn đề thực tế, nhưng nên biết là có.
- `Encode` cấp phát slice mới mỗi lần gọi. Khi ghi log tốc độ cao, nên có thêm dạng ghi
  thẳng vào `io.Writer` hoặc dùng lại buffer để bớt rác cho GC.

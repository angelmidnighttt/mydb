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
| key size | val size | key data | val data |
| 4 bytes  | 4 bytes  |   ...    |   ...    |
```

`key="a"`, `val="bb"` cho ra:

```
[1 0 0 0 2 0 0 0 97 98 98]
 └───┬──┘ └───┬──┘ │  └┬─┘
 len=1    len=2   'a'  "bb"
```

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

	binary.LittleEndian.PutUint32(data[0:4], uint32(len(ent.Key)))
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(ent.Val)))
	copy(data[headerSize:], ent.Key)
	copy(data[headerSize+len(ent.Key):], ent.Val)

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

### Hai loại "hết dữ liệu"

Phân biệt được hai trường hợp này là điều kiện để replay log an toàn:

| Tình huống | Trả về | Ý nghĩa |
|---|---|---|
| Hết stream đúng ranh giới record | `io.EOF` | bình thường — đã đọc xong, dừng vòng lặp |
| Hết stream giữa chừng một record | `io.ErrUnexpectedEOF` | **log hỏng hoặc bị cắt cụt** |

`io.ReadFull` trả `io.EOF` khi chưa đọc được byte nào, và `io.ErrUnexpectedEOF` khi đọc
được một phần. Nhưng có một khe hở: nếu stream kết thúc **đúng ngay sau header**, lời gọi
đọc key bắt đầu từ 0 byte nên `ReadFull` trả `io.EOF` — trong khi thực chất đây là record
bị cắt cụt, vì header đã hứa có key. Hàm `readBody` nâng `io.EOF` thành
`io.ErrUnexpectedEOF` đúng cho trường hợp đó.

Nếu bỏ qua chi tiết này, vòng lặp replay sẽ coi một log bị cắt cụt là log kết thúc bình
thường và **im lặng nuốt mất phần dữ liệu hỏng**. `TestDecodeTruncated` kiểm tra cả bốn
vị trí cắt: giữa header, ngay sau header, giữa key, giữa val.

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

- **Không có checksum.** Một byte bị lật trên đĩa sẽ được đọc ra như dữ liệu hợp lệ,
  không ai phát hiện. Nếu byte hỏng rơi vào phần size, `Decode` sẽ tin theo con số sai đó.
- **Tin tuyệt đối vào size trong header.** Header hỏng báo 4 GiB thì `Decode` cấp phát
  đúng 4 GiB. Với file do chính ta ghi thì tạm chấp nhận; nhận dữ liệu từ network thì đây
  là lỗ hổng DoS, cần đặt trần kích thước.
- **Không có kiểu record.** Chưa phân biệt được "ghi key này" với "xóa key này". Xóa cần
  một record đánh dấu (tombstone), sẽ thêm khi làm log.
- Key/val giới hạn 4 GiB do `uint32`. Không phải vấn đề thực tế, nhưng nên biết là có.
- `Encode` cấp phát slice mới mỗi lần gọi. Khi ghi log tốc độ cao, nên có thêm dạng ghi
  thẳng vào `io.Writer` hoặc dùng lại buffer để bớt rác cho GC.

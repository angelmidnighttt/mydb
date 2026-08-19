# 05 — Data types

Package: [`internal/table`](../internal/table/cell.go)

## Mục tiêu

Từ đây bắt đầu dựng **relational DB** trên nền KV đã có. Giống Excel: một DB chứa nhiều
bảng, một bảng có hàng và cột, mỗi cột có **kiểu dữ liệu** riêng — khác với KV, nơi value
chỉ là bytes không tên tuổi.

Bước này làm viên gạch nhỏ nhất: `Cell` — một ô, một giá trị có kiểu — cùng cách mã hóa
nó thành byte. Hai kiểu trước mắt: `int64` và `[]byte`.

## Kiểu dữ liệu

```go
type CellType uint8

const (
	TypeI64 CellType = 1
	TypeStr CellType = 2
)

type Cell struct {
	Type CellType
	I64  int64
	Str  []byte
}
```

`Type` quyết định trường nào mang giá trị: `I64` hay `Str`. Go không có **union**, nên cả
hai trường luôn tồn tại và một trong hai luôn thừa — khoảng 8 byte lãng phí mỗi cell.
Chỉ lãng phí trong RAM: phần thừa không bao giờ xuống đĩa.

Giá trị `0` cố ý **không** phải kiểu hợp lệ. Nhờ vậy một `Cell` quên gán kiểu bị chặn lại
ngay, thay vì lặng lẽ đi tiếp như `int64`.

## Định dạng

```
int64   | value   |
        | 8 bytes |

[]byte  | length  | data |
        | 4 bytes | ...  |
```

Length đứng trước data vì lý do đã nói ở [03](03-serialization.md#vì-sao-size-phải-đứng-trước):
với kiểu độ dài thay đổi, người đọc cần biết đọc bao nhiêu byte **trước khi** đọc.

### Vì sao không ghi kèm tag kiểu

Trong stream không có byte nào nói cell này là `int64` hay `[]byte`. Cột nào kiểu gì đã
được **schema của bảng** chốt sẵn, cả bên ghi lẫn bên đọc đều biết. Gắn tag vào từng cell
của từng hàng là lặp lại — mỗi hàng một lần — điều schema chỉ cần nói đúng một lần.

Hệ quả nằm ở chữ ký của `Decode`: nó lấy kiểu từ chính cell đang được lấp đầy, nên người
gọi phải gán `cell.Type` theo schema **trước** khi gọi.

```go
cell := table.Cell{Type: table.TypeI64} // schema nói cột này là int64
rest, err := cell.Decode(data)
```

## Encode: nối thêm thay vì cấp phát

```go
func (cell *Cell) Encode(dst []byte) []byte
```

Khác `wal.Entry.Encode` (tự cấp phát rồi trả về slice mới), ở đây kết quả được **append**
vào slice truyền vào, đúng kiểu hàm `append` của Go. Lý do: bước sau sẽ ghép nhiều cell
thành một hàng. Người gọi giữ **một** buffer, cho chạy qua tất cả các cell, rồi tái dùng
chính buffer đó cho hàng tiếp theo — không cấp phát mỗi giá trị một lần.

```go
var data []byte
for i := range row {
	data = row[i].Encode(data)
}
```

Với kiểu không hợp lệ, `Encode` **panic** thay vì trả lỗi: chữ ký không có chỗ để đặt
error, và kiểu sai là bug của người gọi chứ không phải dữ liệu vào bị hỏng. Ghi ra một
cell không kiểu nghĩa là đặt lên đĩa những byte không ai đọc lại được — hỏng ngay tại chỗ
gọi vẫn tốt hơn hỏng lúc đọc, ở một máy khác, sau vài tháng.

## Little-endian và hai kiểu số nguyên

Số nhiều byte phải cố định một thứ tự, và cả project dùng little-endian — lý do đã nói ở
[03](03-serialization.md#vì-sao-là-little-endian-uint32).

Nhắc lại ngắn gọn: một số 32-bit nằm trong thanh ghi chỉ là 32 bit; khi đưa xuống bộ nhớ
nó được chia thành 4 byte. Xếp theo thứ tự tự nhiên (bit thấp ở byte 0) là **little-endian**;
xếp theo thứ tự viết tay (bit cao ở byte 0) là **big-endian**. `0x11223344` thành
`44 33 22 11` với little-endian, `11 22 33 44` với big-endian. Nhiều giao thức mạng cũ
(TCP/IP) dùng big-endian; CPU phổ biến ngày nay là little-endian, nên format mới hầu hết
theo little-endian để khỏi hoán byte. Big-endian vẫn còn một chỗ dùng đặc biệt — **sắp
xếp** — sẽ gặp ở bước sau.

### `binary.LittleEndian` chỉ có `uint64`, vậy `int64` thì sao

```go
cell.I64 = int64(binary.LittleEndian.Uint64(data[0:8]))
```

Ép kiểu giữa `int64` và `uint64` **không dịch chuyển bit nào**, nó chỉ đổi cách CPU diễn
giải cùng chuỗi bit đó:

- Số dương `[0, 2⁶³)` mã hóa giống hệt `uint64`.
- Số âm `[−2⁶³, 0)` ứng với nửa trên của `uint64`, tức `[2⁶³, 2⁶⁴)`.

Nên dải `uint64` bị cắt đôi: 0 và số dương của `int64` nằm ở nửa dưới, số âm nằm ở nửa
trên. Với số dương nhỏ, hai kiểu nhìn giống nhau hoàn toàn ở mức bit. Vì không có phép
biến đổi thật nào xảy ra, ép kiểu qua lại thoải mái.

### Bit dấu và bù hai

Bit cao nhất cho biết số có âm hay không. Nhiều người mặc định máy tính lưu số dạng
**dấu + trị tuyệt đối** — cách đó chỉ tồn tại trên vài máy cũ. Máy tính hiện đại dùng cách
trên, gọi là **bù hai** (two's complement). Nếu dùng dấu + trị tuyệt đối thì sẽ có cả `+0`
lẫn `-0`; thứ đó chỉ có ở số thực dấu phẩy động, không có ở số nguyên.

Bù hai nhìn thấy được ngay trong `TestEncodeLayout`:

| Giá trị | Byte ghi ra |
|---|---|
| `-1` | `[255 255 255 255 255 255 255 255]` — mọi bit bật, đúng bằng `uint64` lớn nhất |
| `math.MinInt64` | `[0 0 0 0 0 0 0 128]` — chỉ bit dấu, little-endian đẩy nó về byte cuối |

`TestRoundTrip` chạy qua `0, 1, -1, MaxInt64, MinInt64` — hai đầu dải là chỗ mà một phép
đổi dấu sai sẽ lộ ra.

## Decode: trả về phần còn lại

```go
func (cell *Cell) Decode(data []byte) (rest []byte, err error)
```

`wal.Entry.Decode` nhận `io.Reader` vì người gọi không biết trước entry dài bao nhiêu.
Ở đây thì ngược lại: cả hàng đã nằm sẵn trong một slice, đọc từng cell chỉ là đi dần từ
trái sang. Nên `Decode` nhận slice và **trả về phần chưa dùng** — người gọi luồn `rest`
qua từng cell cho tới khi hết.

```go
rest := data
for i := range row {
	cell := table.Cell{Type: schema[i]}
	if rest, err = cell.Decode(rest); err != nil {
		return err
	}
}
```

### `Str` là bản sao

Cell giải mã ra giữ **bản sao** chứ không trỏ vào `data`. Buffer đầu vào là thứ người gọi
tái sử dụng — đúng theo thiết kế của `Encode` ở trên — nên một cell trỏ ké vào đó sẽ tự
đổi giá trị sau lưng người dùng. `TestDecodeCopiesInput` ghi đè toàn bộ buffer nguồn sau
khi decode và đòi cell phải giữ nguyên giá trị.

Cùng lý do đó, `Decode` xóa luôn trường không thuộc kiểu vừa đọc (`Str = nil` khi đọc
`int64`, `I64 = 0` khi đọc `[]byte`), để trong cell không còn sót giá trị của lần dùng
trước.

### Hai loại lỗi

| Tình huống | Trả về | Ý nghĩa |
|---|---|---|
| Hết dữ liệu giữa chừng một cell | `io.ErrUnexpectedEOF` | cell bị cắt cụt |
| `cell.Type` không phải kiểu nào | `ErrBadType` | schema và dữ liệu lệch nhau, hoặc cell quên gán kiểu |

Trường `length` **chưa được kiểm chứng** khi đọc tới, y như `size` ở [04](04-write-ahead-log.md):
một record hỏng có thể để lại `0xFFFFFFFF` trong đó. So sánh được ép về `uint64`:

```go
if uint64(size) > uint64(len(data)) {
	return nil, fmt.Errorf("%w: ...", io.ErrUnexpectedEOF)
}
```

So ở `uint64` chứ không ép `size` về `int`: trên bản build 32-bit, `int(0xFFFFFFFF)` cho
ra `-1` và phép kiểm tra sẽ lọt. Đây cũng là lý do không cần giới hạn cấp phát như
`readBody` bên `wal` — độ dài luôn được đối chiếu với lượng byte thật sự có trong tay
trước khi cắt slice, nên không có gì để cấp phát quá tay.

`TestDecodeTruncated` cắt cell ở **mọi** vị trí và đòi tất cả đều trả `io.ErrUnexpectedEOF`
— không có prefix nào được phép giải mã thành công như một cell ngắn hơn.

## Giới hạn hiện tại

- **Mới có 2 kiểu.** Chưa có `float64`, `bool`, `null`, `time`. Kiểu `null` sẽ cần chỗ để
  ghi "ô này rỗng", điều format hiện tại chưa có chỗ chứa.
- **Chưa có hàng, chưa có schema.** `Cell` đứng một mình; ghép cell thành hàng và mô tả
  cột nào kiểu gì là việc của bước sau. Cho tới lúc đó, `Decode` vẫn phải được người gọi
  gán kiểu bằng tay.
- **Chưa nối vào KV.** Package `table` chưa gọi tới `internal/kv` dòng nào — mới là định
  dạng, chưa có chỗ lưu.
- **`[]byte` tối đa 4 GiB** vì length là `uint32`. `Encode` không kiểm tra ngưỡng này:
  chuỗi dài hơn sẽ bị `uint32()` cắt cụt âm thầm. Thực tế không chạm tới, nhưng vẫn là một
  chỗ chưa chặn.
- **Thứ tự byte chưa dùng được để sắp xếp.** Little-endian và bù hai đều làm việc so sánh
  hai cell bằng cách so bytes cho ra kết quả sai. Muốn dùng cell làm key trong B-tree phải
  có một cách mã hóa khác — đây chính là chỗ big-endian quay lại.

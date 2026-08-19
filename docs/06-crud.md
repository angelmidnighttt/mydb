# 06 — CRUD theo primary key

Package: [`internal/table`](../internal/table/db.go) — lần đầu tiên gọi tới [`internal/kv`](../internal/kv/kv.go)

## Mục tiêu

[05](05-data-types.md) mới có `Cell` đứng một mình. Bước này ghép nó thành **hàng**, mô tả
bảng bằng **schema**, và ánh xạ bốn câu lệnh SQL cơ bản xuống KV:

| SQL | `DB` | KV |
|---|---|---|
| `SELECT ... WHERE pk = ?` | `Select` | `Get` |
| `INSERT` | `Insert` | `SetEx(..., ModeInsert)` |
| `UPDATE ... WHERE pk = ?` | `Update` | `SetEx(..., ModeUpdate)` |
| upsert | `Upsert` | `SetEx(..., ModeUpsert)` |
| `DELETE ... WHERE pk = ?` | `Delete` | `Del` |

Ba mode ở [04](04-write-ahead-log.md#update-mode-insert-update-hay-upsert) sinh ra chính
là để cột này có chỗ điền: `INSERT` của SQL không phải là "ghi đè", nó phải **từ chối** khi
key đã tồn tại.

## Một hàng là một cặp KV

Đây là ý chính của cả bước:

```
create table t (a int64, b int64, primary key (b))

hàng (a=7, b=42)
   │
   ├─ cột khóa   ──► KV key : 01 00 00 00 | 74 | 2a 00 00 00 00 00 00 00
   │                          len("t")=1  | 't'| b = 42
   │
   └─ cột còn lại ─► KV val : 07 00 00 00 00 00 00 00
                              a = 7
```

**Cột khóa không xuất hiện trong value.** Nó đã nằm trong key rồi; lưu thêm một bản nữa là
tạo ra hai chỗ phải giữ cho khớp nhau — và hai chỗ thì có ngày lệch nhau.

### Vì sao key có tên bảng đứng trước

Mọi bảng dùng chung một keyspace của KV. Không có tiền tố, hàng số 1 của bảng `users` và
hàng số 1 của bảng `orders` là **cùng một key**, và lần ghi sau âm thầm đè lên hàng của
bảng khác. Tên bảng được đóng khung y hệt một cell `[]byte` — 4 byte độ dài rồi tới nội
dung — vì lý do quen thuộc ở [03](03-serialization.md#vì-sao-size-phải-đứng-trước): phần
đứng sau phải bắt đầu ở một vị trí xác định.

Cách này tốn chỗ: tên bảng lặp lại trong **mọi** key của bảng đó. DB thật gán cho mỗi bảng
một số nguyên 4 byte lấy từ **catalog** (một bảng nội bộ chứa định nghĩa của các bảng khác)
và dùng số đó làm prefix. mydb có catalog từ [10](10-exec.md) nhưng chưa cấp số cho bảng,
nên key vẫn mang cả cái tên.

## `Schema`

```go
type Schema struct {
	Name  string
	Cols  []string
	Types []CellType
	PK    []int      // vị trí các cột khóa, theo thứ tự khóa
}
```

`PK` là **danh sách vị trí**, không phải "n cột đầu tiên". Nhờ vậy

```sql
create table t (a int64, b int64, primary key (b))
```

viết thẳng thành `PK: []int{1}` mà không phải đảo cột `b` lên đầu bảng. Sách gốc chọn cách
ngược lại — bắt khóa phải là các cột đầu — đổi lấy `isPK(i)` chỉ là `i < PKeys` thay vì một
vòng lặp. Khóa nhiều lắm vài cột nên vòng lặp đó không đáng kể, và giữ nguyên thứ tự cột
người dùng khai báo thì dễ hiểu hơn.

Thứ tự trong `PK` **có ý nghĩa**: `PK: {1, 0}` mã hóa cột `b` trước rồi tới `a`, khác hẳn
`PK: {0, 1}`. Đây chính là thứ tự cột trong `primary key (b, a)` của SQL.

## `Row`

```go
type Row []Cell
```

Một `Row` **luôn dài bằng số cột của bảng**, kể cả với thao tác chỉ cần một phần:

- `Select` nhận hàng đã điền sẵn ô khóa, và **lấp nốt các ô còn lại** — ô khóa là input, ô
  còn lại là output.
- `Delete` chỉ đọc ô khóa, các ô khác nhìn cũng không nhìn.
- `Insert` / `Update` / `Upsert` đòi hàng đầy đủ.

Vì độ dài luôn cố định nên **vị trí của một ô chính là cột của nó** — không cần map tên cột,
không cần đánh dấu ô nào đã điền.

## Hai mức kiểm tra

`checkRow` (ghi cả hàng) chặt hơn `checkKey` (chỉ định vị hàng):

| | `Select`, `Delete` | `Insert`, `Update`, `Upsert` |
|---|---|---|
| Schema hợp lệ | ✓ | ✓ |
| Độ dài hàng | ✓ | ✓ |
| Kiểu của ô khóa | ✓ | ✓ |
| Kiểu của ô ngoài khóa | — | ✓ |

Sự lỏng tay đó là cố ý: `Select` sắp ghi đè các ô ngoài khóa, còn `Delete` không đọc chúng.
Bắt người gọi phải điền đúng kiểu cho những ô sẽ bị vứt đi là bắt làm việc thừa.

### Vì sao kiểu phải kiểm ở đây

Vì **không chỗ nào phía sau kiểm được nữa**. Format không ghi type tag ([05](05-data-types.md#vì-sao-không-ghi-kèm-tag-kiểu)),
`Decode` tin tuyệt đối vào những gì schema nói. Đưa một cell `[]byte` vào cột `int64` thì
`Encode` vẫn ghi ra byte hợp lệ, và lần đọc sau vẫn giải mã ra một số — chỉ là số vô nghĩa.
Không lỗi, không cảnh báo, chỉ có dữ liệu sai. `checkCell` là hàng rào duy nhất.

Cùng lý do đó, `DecodeVal` bắt lỗi khi **còn byte thừa** sau cột cuối cùng. Value không nói
nó dài bao nhiêu cột; nếu schema và dữ liệu đã lệch nhau, byte thừa là dấu hiệu duy nhất tự
nó lộ ra.

## Một đường ghi duy nhất

```go
func (db *DB) Insert(schema *Schema, row Row) (bool, error) {
	return db.write(schema, row, kv.ModeInsert)
}
```

Ba hàm ghi khác nhau đúng **một tham số**. Kiểm tra như nhau, key như nhau, value như nhau
— khác nhau ở chỗ KV làm gì khi gặp key đã tồn tại. Cùng tinh thần với `kv.apply` ở
[04](04-write-ahead-log.md): một hành vi thì định nghĩa đúng một chỗ.

Giá trị `bool` trả về mang đúng nghĩa của `SetEx`, kể cả chỗ gợn của nó:

| | Key đã có | Key chưa có |
|---|---|---|
| `Insert` | từ chối → `false` | thêm mới → `true` |
| `Update` | ghi đè → `true` | từ chối → `false` |
| `Upsert` | ghi đè → `true` | thêm mới → `false` |

## Giới hạn hiện tại

- **Schema được lưu từ [10](10-exec.md)** — một **catalog** dưới key `@schema_` + tên bảng.
  Các thao tác hàng ở đây vẫn nhận `*Schema` từ người gọi chứ không tự tra, và đó là cố ý:
  một hàng đọc hay ghi được bằng bất cứ schema nào, kể cả schema catalog chưa từng nghe tới.
  Catalog đó vẫn chưa phải một bảng thật, nên chưa truy vấn được bằng SQL.
- **Chỉ truy cập được bằng primary key.** Chưa có quét toàn bảng, chưa có index, chưa có
  range query, chưa có filter. Cả bốn thứ đó đều cần **duyệt key theo thứ tự**, mà `store`
  hiện tại là một `map` — không có thứ tự nào cả. Đây là chỗ B-tree bước vào.
- **Key mã hóa little-endian nên so sánh bytewise ra sai thứ tự** ([05](05-data-types.md)).
  Ngay cả khi có cấu trúc sắp xếp được, key hiện tại vẫn chưa dùng để sắp xếp được. Range
  query sẽ cần một cách mã hóa khác — big-endian và lật bit dấu.
- **Không có `UPDATE` một phần.** Muốn sửa một cột phải `Select` rồi `Update` cả hàng. Giữa
  hai lời gọi đó có khe hở cho người ghi khác chen vào — khe này chỉ đóng được bằng
  transaction.
- **Tên bảng lặp lại trong mọi key.** Xem phần prefix ở trên.
- **Chưa có kiểu `null`.** Mọi cột đều bắt buộc có giá trị, nên chưa diễn đạt được
  `insert into t (a) values (1)` khi bảng có thêm cột `b`.

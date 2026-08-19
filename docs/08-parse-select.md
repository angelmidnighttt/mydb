# 08 — Ngữ pháp: SELECT

Package: [`internal/sql`](../internal/sql/select.go)

## Mục tiêu

[07](07-tokenizer.md) cắt được chữ thành token. Bước này ghép token thành **câu lệnh**:

```
select a,b from t where c=1 and d='e';
```

```go
StmtSelect{
	table: "t",
	cols:  []string{"a", "b"},
	keys: []NamedCell{
		{column: "c", value: Cell{Type: TypeI64, I64: 1}},
		{column: "d", value: Cell{Type: TypeStr, Str: []byte("e")}},
	},
}
```

Mới có đúng một dạng câu lệnh: **lấy một hàng theo khóa chính**. Đó cũng chính xác là những
gì tầng dưới làm được — `table.DB` ở [06](06-crud.md) chưa quét bảng, chưa có index, nên
ngữ pháp cũng chưa có chỗ cho `order by`, `limit` hay `join`.

## `tryPunctuation`: luật đơn giản hơn keyword

```go
func (p *Parser) tryPunctuation(punct string) bool {
	start := p.skipSpace()
	end := start + len(punct)
	if end > len(p.buf) || p.buf[start:end] != punct {
		return false
	}
	p.pos = end
	return true
}
```

Thiếu hẳn hai luật mà `tryKeyword` phải có:

| | `tryKeyword` | `tryPunctuation` |
|---|---|---|
| Không phân biệt hoa thường | có | không cần — dấu câu không có hoa thường |
| Phải kết thúc ở separator | có | **không cần** |

Vế thứ hai mới đáng nói. Keyword được tạo từ đúng những byte mà một cái tên cũng dùng được,
nên `select` có thể là đầu của `selecting` — phải có luật chặn. Dấu câu thì ngược lại: nó
làm bằng những byte **không bao giờ** nằm trong tên hay số, nên nó luôn tự kết thúc. `1,2`
là ba token mà không cần khoảng trắng nào.

Một cái bẫy để dành: `punct` so nguyên chuỗi nên toán tử nhiều ký tự vẫn chạy, **nhưng**
toán tử ngắn là tiền tố của toán tử dài sẽ khớp trước. Khi có `>=` thì phải thử nó **trước**
`>`, không thì `>=` bị đọc thành `>` rồi bỏ lại `=`.

## Một khuôn hình lặp lại ba lần

Nhìn kỹ thì `select a,b` và `where c=1 and d='e'` là **cùng một hình dạng**:

```
<phần> <dấu ngăn> <phần> <dấu ngăn> <phần> ...
```

| Danh sách | Phần | Dấu ngăn | Kết thúc bởi |
|---|---|---|---|
| Cột | `tryName` | `,` | keyword `from` |
| Điều kiện | `parseEqual` | keyword `and` | hết câu |

Đổi hai chỗ là ra danh sách kia. Đây là điều làm parser viết tay khả thi: một ngữ pháp
**trông** to là một số ít hình dạng, cái này gọi cái kia. Không có gì thông minh, chỉ là
xếp đúng thứ tự.

`parseEqual` là chỗ ba hàm đọc token gặp nhau, đúng theo thứ tự chữ nằm trên dòng:

```go
column, ok := p.tryName()          // c
if !p.tryPunctuation("=") { ... }  // =
return p.parseValue(&out.value)    // 1
```

## Thứ tự thử quyết định ý nghĩa

Vòng lặp cột phải thử `from` **trước** khi thử tên:

```go
for !p.tryKeyword("from") {
	...
	name, ok := p.tryName()
}
```

Vì `from` cũng là một cái tên hợp lệ ([07](07-tokenizer.md#tên-và-keyword-không-phân-biệt-được-nếu-chỉ-nhìn-chữ)).
Đảo thứ tự lại thì `tryName` nuốt mất `from` và danh sách cột không bao giờ kết thúc.

Cùng chuyện đó vẫn còn để lại một chỗ xấu: `select a from where c=1` cho ra `table = "where"`
rồi mới báo lỗi ở bước sau — sai được phát hiện, nhưng bởi luật đứng sau luật thật sự bị vi
phạm. Cách chữa đúng là **reserved word**: cấm hẳn một số từ làm tên.
`TestKeywordsAreStillNames` ghi lại hành vi hiện nay để sau này sửa thì thấy ngay.

## `try` khôi phục con trỏ, `parse` tổ hợp thì không

[07](07-tokenizer.md) nói `tryX` hỏng thì `pos` y nguyên, và `parseValue` cũng vậy. Nhưng
`parseSelect` thì **không**: hỏng ở giữa thì con trỏ nằm lại đúng chỗ nó dừng.

Đó không phải quên, mà là khác về bản chất:

- `tryX` hỏng nghĩa là "chỗ này không phải X" — sẽ có người thử cái khác, nên phải trả lại
  hiện trường nguyên vẹn.
- `parseSelect` hỏng nghĩa là "câu lệnh này viết sai". Đã bắt đầu bằng `select` thì không
  còn khả năng nào khác để thử. Không ai cần hiện trường nguyên vẹn; cái cần là **biết nó
  dừng ở đâu**.

Cả hai cuối cùng cho cùng một thứ: vị trí trong thông báo lỗi trỏ vào chỗ phải sửa.

```
select a b from t where c=1   →  sql: syntax error at 9: expect a comma or from after the column list
select from t where c=1       →  sql: syntax error at 7: expect at least one column between select and from
select a from t               →  sql: syntax error at 15: expect where
```

## Parser không biết gì về schema

`NamedCell` mang **tên** cột, không phải vị trí cột:

```go
type NamedCell struct {
	column string
	value  table.Cell
}
```

Package `sql` không biết bảng `t` có tồn tại không, có cột `c` không, cột đó có phải khóa
chính không, và `int64` có đúng kiểu của nó không. Nó **không được phép** biết: việc của nó
là nói câu lệnh viết gì.

Đối chiếu với schema là việc của tầng cầm schema — chính là `Schema.checkRow` ở
[06](06-crud.md#hai-mức-kiểm-tra). Ranh giới này giữ cho parser không phải sửa mỗi lần
thêm một kiểu dữ liệu, và giữ cho tầng bảng không phải biết SQL trông thế nào.

## Giới hạn hiện tại

- **Parse xong nhưng chưa chạy.** Chưa có gì nối `StmtSelect` xuống `table.DB`. Đó là bước
  kế tiếp, và nó cần một chỗ ánh xạ tên cột sang vị trí cột.
- **`where` bắt buộc, và chỉ có `=` nối bằng `and`.** Không có `>`, `<`, `or`, `not`, `in`,
  `like`, không có ngoặc. `where` không được phép vắng mặt vì thiếu nó là quét cả bảng, mà
  quét bảng thì chưa có.
- **Chỉ có `select`.** `insert`, `update`, `delete` chưa có ngữ pháp, dù `table.DB` đã làm
  được cả năm thao tác từ [06](06-crud.md).
- **Chưa có `select *`.** Phải liệt kê từng cột.
- **Dấu `;` chưa ai ăn.** `parseSelect` dừng ngay trước nó và **không** kiểm tra rằng chuỗi
  đã hết — nên `select a from t where b=2; rác rưởi` vẫn parse thành công. Cần một hàm cấp
  câu lệnh đứng trên, vừa nuốt `;` vừa bắt buộc "hết chuỗi ở đây".
  `TestParseSelectStopsAtTheSemicolon` ghi lại đúng chỗ đó.
- **Keyword vẫn là tên hợp lệ.** Xem phần thứ tự thử ở trên.
- **Không kiểm tra trùng lặp.** `where c=1 and c=2` parse trót lọt thành hai điều kiện mâu
  thuẫn; `select a,a from t` cũng vậy.
- **Số dính liền keyword bị từ chối.** `where c=1and d=2` là lỗi, vì `1and` vi phạm luật
  "số phải kết thúc ở separator" của [07](07-tokenizer.md#int64). Với `d='x'and e=2` thì
  được, vì dấu nháy đóng đã kết thúc token. Đây là hệ quả trực tiếp của một luật ở tầng
  dưới, không phải luật riêng của ngữ pháp.

# 07 — Tokenizer

Package: [`internal/sql`](../internal/sql/parser.go)

## Mục tiêu

Từ đây bắt đầu tầng SQL. Một câu lệnh đi vào dưới dạng chuỗi và phải đi ra dưới dạng cấu
trúc mà database làm việc được:

```
select a,b from t where c=1;
```

```go
StmtSelect{
	table: "t",
	cols:  []string{"a", "b"},
	keys:  []NamedCell{{column: "c", value: Cell{Type: TypeI64, I64: 1}}},
}
```

Chặng đầu của quãng đường đó là **tokenizing**: cắt chuỗi thành những từ mà ngôn ngữ được
tạo thành. SQL có bốn nhóm token:

| Nhóm | Ví dụ | Trạng thái |
|---|---|---|
| Keyword | `select`, `from`, `where` | xong — `tryKeyword` |
| Tên (bảng, cột) | `t`, `a`, `_x9` | xong — `tryName` |
| Ký hiệu | `=` `,` `;` `(` `)` | chưa |
| Số, chuỗi | `1`, `abc` | chưa |

Mỗi nhóm có luật riêng nên mỗi nhóm một hàm. Bước này làm hai nhóm đầu.

## `Parser` là một con trỏ, không phải một mảng token

```go
type Parser struct {
	buf string
	pos int
}
```

Không có bước trung gian "cắt cả câu thành `[]Token` rồi mới phân tích". Chuỗi giữ nguyên,
chỉ có `pos` chạy trên nó. Mọi hàm `tryX` đều đọc từ `pos`, và **chỉ dời `pos` khi khớp**.

Giao kèo đó là toàn bộ thiết kế của bước này:

```
khớp     → trả về true, pos nhảy tới sau token
không khớp → trả về false, pos y nguyên — kể cả space đã bỏ qua để nhìn trước
```

Vì sao "y nguyên" lại quan trọng: một ngữ pháp phần lớn là **danh sách các lựa chọn**. Ở
vị trí này có thể là `select`, có thể là `insert`, có thể là một cái tên. Người gọi thử cái
thứ nhất, không được thì thử cái thứ hai — và không phải dọn dẹp gì cả, vì lần thử hỏng
không để lại dấu vết. Đây chính là **backtracking**, làm được với giá gần như bằng không.

Đó cũng là lý do `skipSpace` **trả về vị trí** thay vì tự dời con trỏ:

```go
func (p *Parser) skipSpace() int {
	pos := p.pos
	for pos < len(p.buf) && isSpace(p.buf[pos]) {
		pos++
	}
	return pos
}
```

Dời con trỏ ngay khi bỏ qua space thì một lần thử hỏng vẫn ăn mất mấy khoảng trắng. Không
sai kết quả, nhưng phá vỡ giao kèo — mà giao kèo còn nguyên vẹn thì mới dễ tin.

## Keyword phải kết thúc ở separator

Đây là luật khiến một keyword là **cả một từ** chứ không phải một tiền tố:

| Chuỗi | `tryKeyword("select")` | Vì sao |
|---|---|---|
| `select a` | ✓ | sau nó là space |
| `select,` | ✓ | sau nó là dấu phẩy |
| `select` | ✓ | hết chuỗi |
| `selecting` | ✗ | `i` còn nối tiếp được vào tên |
| `select_` | ✗ | `_` cũng vậy |
| `select1` | ✗ | chữ số cũng vậy |

Không có luật này thì `selecting` bị đọc thành keyword `select` rồi để lại `ing` làm token
kế tiếp — một câu lệnh sai cú pháp lại chạy được, theo một nghĩa không ai định.

### Vì sao `isSeparator` chặn byte ≥ 128

```go
func isSeparator(ch byte) bool {
	return ch < 128 && !isNameContinue(ch)
}
```

Byte từ 128 trở lên là **một phần của ký tự UTF-8 nhiều byte**. Nếu coi nó là separator thì
`selectá` khớp keyword `select` và bỏ lại nửa ký tự `á`. Vế `ch < 128` nói: cái gì không
phải ASCII thì không kết thúc một từ ở đây.

## Mẹo `ch|32`

```go
func isAlpha(ch byte) bool {
	return 'a' <= (ch|32) && (ch|32) <= 'z'
}
```

Trong ASCII, chữ hoa và chữ thường của cùng một chữ cái chỉ khác nhau đúng **một bit**:

```
'A' = 0x41 = 0100 0001
'a' = 0x61 = 0110 0001
                ^ bit 0x20 — "case bit"
```

Nên `ch|32` gập chữ hoa xuống chữ thường và để nguyên chữ thường. Kiểm một khoảng thay vì
hai. Chỗ tinh tế là **nó không tạo ra dương tính giả**: chỉ hai vùng `0x41..0x5A` và
`0x61..0x7A` rơi vào `a..z` sau khi set bit đó. Hai hàng xóm sát nhất vẫn trượt:

```
'@' = 0x40  →  0x60 = '`'   ngay dưới 'a' (0x61)
'[' = 0x5B  →  0x7B = '{'   ngay trên 'z' (0x7A)
```

`TestCharClasses` giữ đúng hai byte đó lại, vì đó là chỗ duy nhất mẹo này có thể hỏng.

## Tên và keyword không phân biệt được nếu chỉ nhìn chữ

`tryName` trả về `"select"` một cách vui vẻ — nó không biết keyword là gì. Điều đó **đúng**:
một chuỗi chữ cái là tên hay là keyword phụ thuộc vào **vị trí trong ngữ pháp**, không phụ
thuộc vào bản thân chuỗi đó.

Hệ quả thực dụng: ở vị trí nào ngữ pháp chờ một keyword thì phải gọi `tryKeyword` **trước**
`tryName`. Gọi ngược lại thì tên nuốt mất keyword.

Đây cũng là gốc rễ của khái niệm **reserved word** trong SQL thật: `select * from select`
không chạy được, không phải vì `select` không thể là tên bảng về mặt kỹ thuật, mà vì ngữ
pháp không có cách nào biết nên hiểu nó theo nghĩa nào.

## Giới hạn hiện tại

- **Mới có 2 trong 4 nhóm token.** Chưa có ký hiệu (`=` `,` `;` `(` `)`), chưa có số, chưa
  có chuỗi. `TestReadsTheWordsOfAStatement` cho thấy đúng chỗ tokenizer dừng lại hiện nay:
  nó đọc trôi chảy tới dấu phẩy đầu tiên rồi tắc.
- **Chưa có ngữ pháp.** Mới cắt được từ, chưa ghép được câu. Chưa có `StmtSelect`, chưa có
  gì nối xuống `table.DB`.
- **Lỗi chưa có vị trí.** `tryX` chỉ trả `false`, không nói sai ở đâu và vì sao. Một câu
  lệnh sai cú pháp cần báo được "dòng 1, cột 14: chờ tên bảng" — muốn vậy phải có một kiểu
  lỗi mang theo `pos`, chứ `bool` thì không chở được gì.
- **Chưa có comment, chưa có tên trong dấu nháy.** `-- ghi chú` và `"my column"` đều chưa
  đọc được.
- **Tên chỉ nhận ASCII.** Đọc theo byte nên cột tên tiếng Việt không đặt được. Với keyword
  thì đó là điều đúng đắn, với tên bảng và tên cột thì là một giới hạn.

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
| Số, chuỗi | `1`, `-7`, `'abc'` | xong — `parseInt`, `parseString` |
| Ký hiệu | `=` `,` `;` `(` `)` | xong — `tryPunctuation` |

Mỗi nhóm có luật riêng nên mỗi nhóm một hàm.

## `Parser` là một con trỏ, không phải một mảng token

```go
type Parser struct {
	buf string
	pos int
}
```

Không có bước trung gian "cắt cả câu thành `[]Token` rồi mới phân tích". Chuỗi giữ nguyên,
chỉ có `pos` chạy trên nó. Mọi hàm đều đọc từ `pos`, và **chỉ dời `pos` khi thành công**.

Giao kèo đó là toàn bộ thiết kế của bước này:

```
được    → pos nhảy tới sau token
không được → pos y nguyên — kể cả space đã bỏ qua để nhìn trước
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

### `try` khác `parse` ở chỗ nào

| | Trả về | Nghĩa |
|---|---|---|
| `tryX` | `bool` | "chỗ này có phải X không?" — không phải cũng bình thường, thử cái khác |
| `parseX` | `error` | "chỗ này **phải** là X" — không phải là lỗi cú pháp, phải báo cho người dùng |

Tới lúc ngữ pháp gọi `parseValue` thì nó đã biết chắc chỗ đó là một giá trị; cái gì khác
không còn là "một lựa chọn khác" nữa mà là câu lệnh viết sai. Cả hai loại vẫn giữ chung một
giao kèo về `pos`, và với `parseX` nó thêm một tác dụng: **vị trí trong thông báo lỗi và vị
trí con trỏ luôn khớp nhau**.

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

## Giá trị: một byte quyết định đi đường nào

```go
ch := p.buf[pos]
switch {
case ch == '"' || ch == '\'':
	return p.parseString(out)
case isDigit(ch) || ch == '-' || ch == '+':
	return p.parseInt(out)
default:
	return p.errorf(pos, "expect a value, found %q", ch)
}
```

Nhìn **một byte** là biết gọi hàm nào. Không thử rồi quay lui, không cần nhìn xa hơn. Phần
lớn ngôn ngữ lập trình được thiết kế để đọc được theo kiểu đó — mỗi chỗ rẽ chỉ cần nhìn
trước một token là quyết được, người ta gọi là **LL(1)** — và chính vì vậy một parser viết
tay mới ngắn được như thế này. Ngôn ngữ nào không có tính chất đó thì parser phải nhìn xa,
phải quay lui, và dài ra rất nhanh.

Kết quả đi thẳng vào `table.Cell` của [05](05-data-types.md): đây là chỗ tầng SQL chạm vào
tầng quan hệ lần đầu.

## `int64`

Luật gốc rất gọn: `+` hoặc `-` tùy chọn, rồi chữ số. Ở đây thêm hai chỗ nghiêm hơn:

**Số phải kết thúc ở separator.** `1a` bị từ chối chứ không đọc thành `1` rồi bỏ lại `a`.
Cùng lý do với `selecting`: một lỗi gõ không được phép biến thành hai token đều hợp lệ.

**Dấu chấm bị chặn riêng.** `.` vốn *là* separator, nên nếu không chặn thì `1.5` đọc trót
lọt thành `1` và bỏ lại `.5` cho người sau — một giá trị **âm thầm mất phần thập phân**.
Chưa có `float64` thì nói thẳng ra vẫn hơn là làm sai lặng lẽ.

Còn tràn số thì để `strconv.ParseInt` lo. Cú pháp đã được kiểm trước khi gọi, nên lỗi duy
nhất nó còn có thể trả về là "không vừa 64 bit":

```
9223372036854775808  →  sql: syntax error at 0: 9223372036854775808 does not fit in an int64
```

## Chuỗi

- Mở bằng `"` hoặc `'`, và **đóng bằng đúng dấu đã mở nó**. Nhờ vậy loại nháy còn lại thành
  chữ bình thường ở bên trong: `'say "hi"'` không cần escape gì cả.
- `\` escape byte đứng ngay sau, và chỉ ba escape là hợp lệ: `\'`, `\"`, `\\`.

### Vì sao `\n` bị từ chối chứ không thành chữ `n`

Đây là chỗ đáng cân nhắc nhất của cả bước. Nếu hôm nay `\n` nghĩa là chữ `n`, ngày mai thêm
escape thật vào thì `"a\nb"` **đổi nghĩa** — câu lệnh cũ vẫn chạy, vẫn không báo gì, chỉ ra
kết quả khác. Từ chối ngay từ đầu khiến mọi escape thêm về sau đều là **thêm thuần túy**:
cái gì đang chạy được thì vẫn chạy đúng như cũ, cái gì đang bị từ chối thì bắt đầu chạy.

Nguyên tắc chung đằng sau: khi chưa chắc một cú pháp nên mang nghĩa gì, **cấm** nó dễ sửa
hơn là gán đại cho nó một nghĩa.

## Lỗi mang theo vị trí

```
  1a      →  sql: syntax error at 3: a number cannot run straight into 'a'
  "abc    →  sql: syntax error at 2: the string opened here is never closed
  *       →  sql: syntax error at 2: expect a value, found '*'
```

Offset trỏ vào **chỗ phải sửa**, không phải chỗ phát hiện ra. Với chuỗi không đóng, chỗ phải
sửa là dấu nháy mở ở đầu; báo ở cuối câu thì người đọc biết là thiếu nháy nhưng không biết
thiếu của cái nào.

Người gọi chỉ cần một `errors.Is(err, ErrSyntax)` để phân biệt "câu lệnh viết sai" với
"database có vấn đề" — cùng cách dùng sentinel như `ErrBadRow` hay `ErrBadMode` ở các tầng
dưới. Chi tiết nằm trong message chứ không đẻ thêm kiểu lỗi.

## Giới hạn hiện tại

- **Cắt token xong, ghép câu là chuyện của [08](08-parse-select.md).** Bốn nhóm token đã
  đủ; `StmtSelect` và ngữ pháp nằm ở bước sau.
- **Mới có 3 escape.** `\n`, `\t`, `\xFF` và escape unicode đều bị từ chối —
  cố ý, xem phần trên.
- **Chưa có `float64`, `null`, `bool`.** Ba thứ này thiếu ở tầng `Cell` từ
  [05](05-data-types.md) nên tokenizer cũng không đọc được.
- **Chưa có comment, chưa có tên trong dấu nháy.** `-- ghi chú` và `"my column"` đều chưa
  đọc được. Chú ý chỗ chồng lấn: `"..."` hiện luôn là chuỗi, nên nếu sau này muốn nó là tên
  cột theo chuẩn SQL thì phải chọn một trong hai.
- **Tên chỉ nhận ASCII.** Đọc theo byte nên cột tên tiếng Việt không đặt được. Với keyword
  thì đó là điều đúng đắn, với tên bảng và tên cột thì là một giới hạn.
- **Vị trí lỗi mới là offset byte.** Chưa quy ra dòng và cột, cũng chưa in ra được đoạn văn
  bản có gạch chân — hai thứ cần khi câu lệnh dài hơn một dòng.

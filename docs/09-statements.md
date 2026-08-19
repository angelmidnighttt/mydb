# 09 — Bốn câu lệnh còn lại

Package: [`internal/sql`](../internal/sql/stmt.go)

## Mục tiêu

[08](08-parse-select.md) đọc được `select`. Bước này thêm bốn câu lệnh nữa và một hàm đứng
trên tất cả để nhận ra câu lệnh nào là câu lệnh nào:

```sql
create table t (a int64, b string, c string, primary key (b, c));
insert into t values (1, 'x', 'y');
update t set a = 1 where b = 'x' and c = 'y';
delete from t where b = 'x' and c = 'y';
```

Tới đây tầng SQL phủ đúng những gì `table.DB` ở [06](06-crud.md) làm được: tạo bảng, và
bốn thao tác một hàng theo khóa chính.

## `tryKeyword` nhận nhiều từ

```go
func (p *Parser) tryKeyword(kws ...string) bool
```

Tên của câu lệnh không phải lúc nào cũng một từ: `create table`, `insert into`,
`delete from`. Điểm mấu chốt là **được ăn cả, ngã về không**:

```go
start := p.pos
for _, kw := range kws {
	if !p.tryOneKeyword(kw) {
		p.pos = start      // ← trả lại cả những từ đã khớp
		return false
	}
}
```

Không có dòng `p.pos = start` thì `create index i on t (a)` sẽ bị `tryKeyword("create",
"table")` **ăn mất chữ `create`** rồi mới trả `false`. Nhánh tiếp theo trong `parseStmt` bắt
đầu đọc từ giữa tên câu lệnh, và thông báo lỗi trỏ vào chữ `index` thay vì vào đầu câu.

`TestParseStmtLeavesUnmatchedNamesAlone` kiểm đúng điều đó: lỗi phải trỏ về offset 0.

Đây là giao kèo `tryX` của [07](07-tokenizer.md#parser-là-một-con-trỏ-không-phải-một-mảng-token)
áp dụng ở mức cao hơn một bậc — trước là một token, giờ là một dãy token.

## `parseStmt`: một hai từ là đủ

```go
switch {
case p.tryKeyword("select"):        → StmtSelect
case p.tryKeyword("create", "table"): → StmtCreateTable
case p.tryKeyword("insert", "into"):  → StmtInsert
case p.tryKeyword("update"):          → StmtUpdate
case p.tryKeyword("delete", "from"):  → StmtDelete
default:                              → lỗi
}
```

Không cần nhìn xa hơn, không cần quay lui, không cần bảng trạng thái. Đây là điểm chính của
cả chương: **SQL — và phần lớn ngôn ngữ người ta viết tay — được thiết kế để một parser
tầm thường như thế này là đủ**. Lý thuyết trình biên dịch sinh ra cho những ngữ pháp không
có tính chất đó, không phải cho ngữ pháp nào cũng cần.

Cùng một ý với `parseValue` ở [07](07-tokenizer.md#giá-trị-một-byte-quyết-định-đi-đường-nào)
nhìn một byte để biết đọc số hay đọc chuỗi — chỉ khác quy mô: ở đây là cả một câu lệnh.

### Hệ quả: `parseSelect` phải sửa

`parseStmt` **đã ăn** chữ `select` rồi (nó buộc phải ăn thì mới biết rẽ đâu). Nên mọi hàm
`parseX` cấp câu lệnh giờ bắt đầu từ **ngay sau tên của chính nó**:

```go
// trước
func (p *Parser) parseSelect(out *StmtSelect) error {
	if !p.tryKeyword("select") { ... }     // ← bỏ dòng này
	...
```

Đó là một thay đổi nhỏ nhưng phá vỡ giao diện: toàn bộ test cũ gọi `parseSelect` với chuỗi
bắt đầu bằng `select` đều phải sửa. Đa số chuyển sang gọi `parseStmt` — vốn cũng đúng hơn,
vì đó mới là cửa vào thật.

## Sáu danh sách, một khuôn hình

Nhìn hết các câu lệnh thì thấy chúng gần như chỉ là **danh sách** lồng vào nhau, và mọi danh
sách đều đọc theo đúng một cách: *một phần, một dấu ngăn, lại một phần, cho tới khi dấu ngăn
không tới nữa*.

| Danh sách | Phần | Dấu ngăn | Kết thúc bởi |
|---|---|---|---|
| Cột của `select` | `tryName` | `,` | keyword `from` |
| `where` | `parseEqual` | keyword `and` | hết câu |
| `set` của `update` | `parseEqual` | `,` | keyword `where` |
| `values` của `insert` | `parseValue` | `,` | `)` |
| Thân `create table` | cột **hoặc** `primary key` | `,` | `)` |
| Danh sách tên trong ngoặc | `tryName` | `,` | `)` |

Đổi ba ô là ra một danh sách khác. Cả bốn câu lệnh mới cộng lại chưa tới 150 dòng, và phần
lớn số dòng đó là thông báo lỗi.

Chỗ đắt nhất của bảng này là hai hàng `where` và `set`: **cùng gọi `parseEqual`**, chỉ khác
dấu ngăn — nhưng nghĩa thì ngược nhau. `set a = 1` là *ghi cái này*; `where b = 'x'` là *vào
hàng nào*. Cú pháp giống hệt, ý nghĩa do vị trí trong câu quyết định, đúng như tên với
keyword ở [07](07-tokenizer.md#tên-và-keyword-không-phân-biệt-được-nếu-chỉ-nhìn-chữ).

## `create table`: đọc ngoặc như một danh sách duy nhất

Trong ngoặc có hai loại thứ khác nhau — định nghĩa cột, và mệnh đề `primary key`. Cách dễ
nghĩ nhất là "đọc cột trước, rồi đọc khóa". Cách ở đây là coi cả ngoặc là **một** danh sách,
mỗi phần tử là cột *hoặc* khóa:

```go
for {
	if p.tryKeyword("primary", "key") {
		... parseNameList
	} else {
		... parseColumn
	}
	if !p.tryPunctuation(",") { break }
}
```

Không dài hơn, mà được thêm một thứ miễn phí: **khóa viết ở đâu trong danh sách cũng được**,
đúng như SQL thật cho phép. `TestParseCreateTable/the_key_written_first` giữ chỗ đó.

Bắt buộc phải có đúng một `primary key`. Không có khóa thì hàng không có địa chỉ — `Schema`
ở [06](06-crud.md) cũng từ chối chuyện đó.

## Parser vẫn không biết gì về schema

Ba thứ nó **không** kiểm, dù trông như có thể:

| Không kiểm | Vì sao | Ai kiểm |
|---|---|---|
| `primary key (b)` mà không có cột `b` | tên chỉ là chữ, dù nó nằm ngay trong cùng câu lệnh | tầng chạy, lúc đổi tên thành vị trí |
| `insert` thừa/thiếu giá trị | không biết bảng có mấy cột | `Schema.checkRow` ở [06](06-crud.md#hai-mức-kiểm-tra) |
| `where c = 1` mà `c` là cột string | không biết kiểu của cột | `Schema.checkCell` |

Trường hợp đầu là cái đáng bàn: dữ liệu cần để kiểm **nằm ngay trong câu lệnh đó**, parser
hoàn toàn kiểm được. Vẫn không kiểm, vì vạch ranh giới ở "cú pháp / ngữ nghĩa" thì mỗi bên
chỉ có một việc, còn vạch ở "cái gì tiện thì làm luôn" thì không ai biết trách nhiệm nằm ở
đâu nữa. Tầng chạy dù sao cũng phải đổi tên cột thành vị trí — và đó chính là chỗ một cái
tên không khớp tự lộ ra.

## `any` là chỗ Go thiếu

```go
func (p *Parser) parseStmt() (any, error)
```

Go không có cách nói "trả về **một trong năm** kiểu này". Cái giá là người gọi phải
`switch stmt.(type)` mà **không có gì kiểm tra là đã xét đủ năm nhánh** — quên một nhánh thì
compiler im lặng, chương trình chạy vào `default` lúc runtime.

Khá hơn được một chút: khai báo một interface có method không export, cho năm struct cùng
cài đặt. Kiểu ngoài package không chui vào tập hợp được nữa, dù vẫn không có kiểm tra đủ
nhánh. Ngôn ngữ có sum type (Rust `enum`, TypeScript union) thì compiler bắt được cả hai.

## Giới hạn hiện tại

- **Chạy được từ [10](10-exec.md)**, chỗ đổi tên cột sang vị trí cột và chỗ lưu schema đều
  ở đó.
- **Dấu `;` vẫn chưa ai ăn**, và `parseStmt` không kiểm tra chuỗi đã hết. Cần một hàm cấp
  trên nữa: nuốt `;`, đòi hết chuỗi, và về sau là đọc nhiều câu lệnh trong một chuỗi.
- **`insert` không có danh sách cột.** `insert into t (a, b) values (1, 2)` chưa viết được;
  giá trị phải đủ và đúng thứ tự khai báo. `parseNameList` đã sẵn sàng cho việc đó.
- **`where` chỉ có `=` nối bằng `and`**, và bắt buộc phải có. Xem
  [08](08-parse-select.md#giới-hạn-hiện-tại).
- **Chưa có `drop table`, `alter table`, `create index`.** Thêm một nhánh vào `parseStmt` là
  xong phần nhận diện — phần khó nằm ở tầng dưới.
- **Không kiểm trùng lặp.** `create table t (a int64, a string, ...)` parse trót lọt và bị
  `Schema.check` chặn lúc chạy ([10](10-exec.md)); còn `update t set a=1, a=2` thì không ai
  chặn, giá trị viết sau thắng.
- **Kiểu trong SQL gọi là `string`, trong `Cell` gọi là `bytes`.** Cùng một thứ, hai cái
  tên; test in ra `b bytes` cho cột khai báo `b string`. Chưa đáng sửa, nhưng đáng biết.
- **`StmtCreatTable` được đổi thành `StmtCreateTable`** so với đề bài — chỗ đó là lỗi gõ,
  và mang một lỗi gõ vào tên kiểu thì nó ở lại rất lâu.

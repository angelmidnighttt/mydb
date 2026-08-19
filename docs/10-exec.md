# 10 — Chạy câu lệnh

Package: [`internal/sql`](../internal/sql/exec.go) + [`internal/table`](../internal/table/catalog.go)

## Mục tiêu

Chín bước trước dựng hai nửa quay lưng vào nhau. Bước này ghép chúng lại:

```
"select a from t where b='x' and c='y'"
        │
        │  parseStmt                     [07][08][09]
        ▼
   *StmtSelect{table:"t", cols:["a"], keys:[b='x', c='y']}
        │
        │  Exec        ← BƯỚC NÀY: đổi TÊN cột thành VỊ TRÍ cột
        ▼
   db.Select(schema, row)                [06]
        │
        ▼
   kv.Get(key)  →  wal + store           [02][03][04]
```

Toàn bộ công việc của bước này gói trong một câu: **đổi tên thành vị trí**. Parser cố tình
không được biết schema ([09](09-statements.md#parser-vẫn-không-biết-gì-về-schema)), tầng
bảng cố tình không biết SQL — chỗ nối là nơi duy nhất biết cả hai.

## Vì sao `Exec` không phải là method của `table.DB`

Sách gốc để `DB`, `Schema`, `Row` và `Stmt*` chung một package nên viết được
`db.ExecStmt(stmt)`. Ở đây chúng nằm hai package, và chiều phụ thuộc là `sql → table`. Cho
`table.DB` một method nhận `*StmtSelect` sẽ bắt `table` import `sql`, tức là **vòng lặp
import** — Go từ chối thẳng.

Nên `Exec` là hàm tự do trong `sql`:

```go
func Exec(db *table.DB, stmt any) (Result, error)
```

Đây không phải giải pháp chữa cháy: nó nói đúng chiều phụ thuộc. Tầng SQL biết về tầng
bảng; tầng bảng không cần biết trên nó có gì.

Nhưng **catalog thì ngược lại** — nó đi vào `table`, vì thứ nó lưu là `Schema`, và `Schema`
là của tầng bảng. Câu hỏi "cái này thuộc về đâu" trả lời bằng "nó nói về cái gì", không phải
"ai gọi nó".

## Catalog: schema cũng chỉ là dữ liệu

```go
func (db *DB) CreateTable(schema *Schema) error
func (db *DB) GetSchema(name string) (*Schema, error)
```

Định nghĩa bảng nằm dưới key `@schema_` + tên bảng, nội dung là JSON. Ba điểm:

**Vì sao JSON, giữa một project tự viết mọi định dạng.** Đây là chỗ duy nhất trong database
không dùng định dạng nhị phân riêng, và nó xứng đáng được ngoại lệ: mỗi bảng đúng một bản
ghi, đọc một lần cho cả đời tiến trình, và là thứ **phải sống sót qua các phiên bản**. Định
dạng tự mô tả kiếm lại được chỗ nó tốn ở đúng những chỗ như vậy. Ngược lại, hàng thì có
hàng triệu, nên từng byte ở đó mới đáng cân đo.

**`ModeInsert` được dùng đúng chỗ nó sinh ra.** Tạo lại một bảng đã có bị từ chối, không
phải ghi đè:

```go
added, err := db.KV.SetEx(schemaKey(schema.Name), val, kv.ModeInsert)
if !added {
	return fmt.Errorf("%w: %s", ErrTableExists, schema.Name)
}
```

Ghi đè định nghĩa là hỏng dữ liệu kiểu tệ nhất: mọi hàng đã ghi theo định nghĩa cũ sẽ được
đọc theo định nghĩa mới, mà việc đó **không báo lỗi** — nó chỉ đọc ra rác. Chính là mối lo
đã ghi ở [06](06-crud.md#giới-hạn-hiện-tại), và `ModeInsert` từ [04](04-write-ahead-log.md#update-mode-insert-update-hay-upsert)
là câu trả lời.

**Đọc lên rồi vẫn phải kiểm lại.** `GetSchema` gọi `schema.check()` sau khi `json.Unmarshal`.
Giữa lúc ghi xuống và lúc đọc lên không có ai canh, mà các thao tác hàng thì **lấy `Cols` và
`Types` theo chỉ số nằm trong `PK`**. Một định nghĩa hỏng mà lọt qua đây sẽ hiện nguyên hình
sau đó bằng một panic index out of range, ở một chỗ chẳng liên quan gì.

### Cache nạp lười, không nạp lúc tạo

`CreateTable` **không** bỏ schema vào cache dù đang cầm nó trên tay. Lần `GetSchema` đầu
tiên tự đọc từ store và tự decode.

Cái giá là một lần parse JSON của một value nhỏ vốn đã nằm trong RAM. Cái được là cache chỉ
chứa đúng cái store chứa — không bao giờ chứa một struct mà **người gọi vẫn còn con trỏ tới
và có thể sửa sau lưng**. `Schema` có ba slice bên trong, nên "copy" nó không đủ để cắt
liên kết; decode lại từ byte thì đủ.

### Keyspace dùng chung

Key của hàng mở đầu bằng 4 byte độ dài tên bảng ([06](06-crud.md#một-hàng-là-một-cặp-kv)),
key của catalog mở đầu bằng `@sch`. Để một key hàng bắt đầu như vậy thì tên bảng phải dài
0x68636740 ký tự — gần 1,8 tỷ. An toàn, nhưng an toàn **nhờ số học chứ không nhờ luật nào**.
Muốn chắc thì phải thêm một byte không gian tên vào đầu mọi key.

## Ba việc của tầng nối

| Hàm | Đổi cái gì thành cái gì |
|---|---|
| `lookupColumns` | `select c,a` → `[2, 0]`, theo thứ tự **câu lệnh viết** |
| `makePKey` | `where b='x' and c='y'` → `Row` đã điền ô khóa |
| `subsetRow` | hàng đầy đủ → chỉ những cột được hỏi |

`lookupColumns` giữ thứ tự của câu lệnh chứ không phải thứ tự của bảng: `select c,a` khác
`select a,c`, và `TestSelectColumnOrder` giữ chỗ đó.

### `makePKey` đòi **đúng** toàn bộ khóa chính

Không thiếu, không thừa, không lệch:

| `where` | Kết quả | Vì sao |
|---|---|---|
| `b='x' and c='y'` | ✓ | đủ khóa |
| `b='x'` | ✗ | nửa khóa không chỉ một hàng, nó chỉ **một dải** hàng |
| `a=1 and b='x'` | ✗ | `a` không thuộc khóa — đó là **lọc**, không phải tra cứu |
| `b='x' and b='y'` | ✗ | đếm thì khớp, nhưng `c` không được nhắc tới |

Hai trường hợp giữa đều cần cùng một thứ: **duyệt hàng**. Mà duyệt hàng thì `store` — một
`map` — chưa làm được. Từ chối ở đây trung thực hơn là trả lời sai.

Vòng lặp điền khóa chạy theo `schema.PK` chứ không theo mệnh đề `where`. Nhờ vậy mọi cột
khóa **buộc phải** được nhắc tới, và kết hợp với phép đếm ở trên thì trường hợp "nhắc một
cột hai lần" tự lộ ra mà không cần kiểm riêng.

## `UPDATE` là đọc-sửa-ghi

```go
row, _ := makePKey(schema, stmt.keys)
found, _ := db.Select(schema, row)      // ← đọc hàng cũ lên trước
if !found { return 0, nil }
for i, set := range stmt.value {
	row[sets[i]] = set.value             // ← đè lên những cột được SET
}
db.Update(schema, row)
```

Phải đọc trước vì `table.DB` **ghi cả hàng một lượt**: những cột mà câu lệnh không nhắc tới
vẫn phải có mặt trong cái được ghi xuống, và chỗ duy nhất lấy chúng là hàng đang nằm đó.
`TestUpdateKeepsTheColumnsItDoesNotName` giữ chỗ này.

Hệ quả phải nói thẳng: **một lần đọc và một lần ghi, không có khóa ở giữa, thì không
atomic**. Hai `UPDATE` vào cùng một hàng có thể nuốt mất nhau. Chuyện đó đóng lại bằng
transaction, không đóng lại bằng gì khác.

**Không cho `set` cột khóa.** Khóa là địa chỉ của hàng, nên ghi vào khóa không phải sửa một
hàng mà là **dời** nó — một lệnh xóa cộng một lệnh chèn đội lốt `UPDATE`. Từ chối vẫn trung
thực hơn là làm một nửa.

## Bốn tầng lỗi

Đây là chỗ thấy rõ nhất giá trị của việc chia tầng: **mỗi loại sai được bắt đúng một lần,
ở tầng biết đủ để bắt nó**.

| Sai ở đâu | Lỗi | Ai bắt |
|---|---|---|
| Chữ không phải SQL | `ErrSyntax` | parser [07](07-tokenizer.md) |
| Bảng không tồn tại | `table.ErrNoTable` | catalog |
| Cột không tồn tại, `where` không phải khóa, `insert` sai số lượng | `sql.ErrBadStatement` | tầng nối |
| Kiểu giá trị không khớp cột | `table.ErrBadRow` | `Schema.checkRow` [06](06-crud.md#hai-mức-kiểm-tra) |

Hàng cuối đáng chú ý: `insert into t values ('one','x','y')` khi cột `a` là `int64` — tầng
nối **không** kiểm kiểu, dù nó có cả schema lẫn giá trị trong tay. Nó chuyển thẳng xuống, và
`db.Insert` từ chối bằng đúng cái kiểm đã bảo vệ mọi người gọi khác từ [06](06-crud.md).
Viết lại phép kiểm đó ở đây là tạo ra chỗ thứ hai để hai bên lệch nhau.

## Ba chỗ khác đề bài

- **`SQLResult` → `Result`.** `sql.SQLResult` lắp bắp; trong Go tên package đã là một nửa
  của tên.
- **`db.ExecStmt(stmt)` → `Exec(db, stmt)`.** Bắt buộc, xem phần vòng lặp import ở trên.
- **`panic("unreachable")` → trả lỗi.** `Exec` nhận `any` và được export, nên một kiểu lạ là
  thứ **người gọi đưa vào**, không phải bug nội bộ. Code nhận đầu vào xấu thì báo lỗi, không
  làm sập tiến trình. Đây cũng chính là cái giá của `any` đã nói ở
  [09](09-statements.md#any-là-chỗ-go-thiếu): compiler không đảm bảo nhánh `default` là
  không thể tới.

## Giới hạn hiện tại

- **Chưa có cửa vào nhận chuỗi.** `Exec` được export nhưng `parseStmt` thì không, nên từ
  ngoài package chưa ai chạy được một câu SQL. Còn thiếu một hàm `Parse(text) (any, error)`
  — và nó là chỗ phải quyết định nốt chuyện dấu `;` cùng việc bắt buộc "hết chuỗi ở đây",
  vốn treo từ [08](08-parse-select.md#giới-hạn-hiện-tại). Hiện chỉ test gọi được, vì test
  nằm trong cùng package.
- **`SELECT` tối đa một hàng.** `Result.Values` là slice để sẵn sàng cho nhiều hàng, nhưng
  chưa có gì trả về nhiều hơn một.
- **`UPDATE` không atomic.** Xem phần đọc-sửa-ghi.
- **Chưa có `drop table`.** Catalog chỉ thêm được, không xóa được — và nếu xóa được thì còn
  phải xóa mọi hàng của bảng đó, mà duyệt hàng của một bảng thì chưa làm được.
- **Định dạng catalog gắn với tên trường Go.** Đổi tên `Schema.Cols` là đọc hỏng mọi
  database cũ. Cần tag JSON, hoặc số phiên bản, hoặc cả hai.
- **Catalog chưa phải một bảng.** Nó là một value JSON dưới một key, nên không truy vấn được
  bằng SQL. Database thật lưu catalog **trong chính các bảng**, để `select * from tables`
  chạy được như mọi câu khác.
- **Cache không bao giờ bị đẩy ra.** Mỗi bảng từng đọc là một `Schema` giữ trong RAM tới lúc
  tiến trình chết. Với số bảng thực tế thì không sao.

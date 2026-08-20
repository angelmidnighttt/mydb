# mydb

[![CI](https://github.com/angelmidnighttt/mydb/actions/workflows/ci.yml/badge.svg)](https://github.com/angelmidnighttt/mydb/actions/workflows/ci.yml)

Một key-value store viết từ đầu bằng Go, làm để học cách một database hoạt động bên dưới.

**Trạng thái:** đã có phần lõi của một database — log + checksum + fsync — và tầng quan hệ
ở mức CRUD theo khóa chính. Ghi thành công là chắc chắn không mất, kể cả khi mất điện.
Câu lệnh SQL chạy được từ đầu tới đĩa: parse, đối chiếu schema, ghi qua log. Chưa có index,
chưa quét được bảng, chưa có network layer.

## Cấu trúc

```
mydb/
├── main.go                    # demo tạm thời: mở db, ghi một key rồi thoát
├── Makefile                   # build / test / run
├── docs/                      # tài liệu, đọc theo thứ tự số
└── internal/
    ├── store/                 # KV trong RAM: mảng sắp xếp + binary search
    ├── wal/                   # định dạng record + file log append-only
    ├── kv/                    # ghép log với store: ghi log trước, replay khi mở
    ├── table/                 # tầng quan hệ: cell, schema, row, CRUD, catalog
    └── sql/                   # tầng SQL: token, ngữ pháp, và chạy câu lệnh
```

## Chạy thử

```sh
make run     # chạy demo trong main.go
make test    # chạy toàn bộ test
make check   # fmt + vet + test
make help    # xem tất cả target
```

## Tài liệu

Đọc theo thứ tự — xem [docs/](docs/README.md) để biết mục lục đầy đủ.

1. [Tổng quan](docs/01-tong-quan.md) — mydb là gì, lộ trình
2. [In-memory store](docs/02-in-memory-store.md) — `internal/store`
3. [Serialization](docs/03-serialization.md) — `internal/wal`
4. [Write-ahead log](docs/04-write-ahead-log.md) — `internal/wal` + `internal/kv`
5. [Data types](docs/05-data-types.md) — `internal/table`
6. [CRUD](docs/06-crud.md) — `internal/table` + `internal/kv`
7. [Tokenizer](docs/07-tokenizer.md) — `internal/sql`
8. [Ngữ pháp: SELECT](docs/08-parse-select.md) — `internal/sql`
9. [Bốn câu lệnh còn lại](docs/09-statements.md) — `internal/sql`
10. [Chạy câu lệnh](docs/10-exec.md) — `internal/sql` + `internal/table`
11. [Mảng sắp xếp](docs/11-sorted-store.md) — `internal/store`

## Bước tiếp theo

Mã hóa key **so sánh được bằng bytes** — big-endian cộng lật bit dấu cho `int64`, escape
cộng terminator cho chuỗi — kèm `Row.DecodeKey`. [11](docs/11-sorted-store.md) đã sắp xếp
bộ nhớ, nhưng key hiện tại sắp ra sai thứ tự nên range query chưa nói tới được.

Rồi API duyệt (`Seek`/`Next`) ở tầng store, `Scan` ở tầng bảng, và ngữ pháp `> < order by`.
Song song đó vẫn còn thiếu `Parse(text)` để ngoài package chạy được SQL.

Xa hơn: LSM-tree để bỏ giả định "toàn bộ dữ liệu vừa trong RAM" — mảng sắp xếp vừa dựng
chính là hình dạng nó lớn lên thành. Rồi server + giao thức, để client nói chuyện với db
qua network thay vì chỉ gọi hàm trong cùng process.

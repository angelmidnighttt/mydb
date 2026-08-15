# mydb

Một key-value store viết từ đầu bằng Go, làm để học cách một database hoạt động bên dưới.

**Trạng thái:** đã có KV in-memory và định dạng serialize cho entry. Chưa có persistence,
chưa có network layer — tắt chương trình là mất dữ liệu.

## Cấu trúc

```
mydb/
├── main.go                    # demo tạm thời, chạy vài lệnh Set/Get rồi thoát
├── Makefile                   # build / test / run
├── docs/                      # tài liệu, đọc theo thứ tự số
└── internal/
    ├── store/                 # KV trong RAM: map + RWMutex
    └── wal/                   # định dạng record cho write-ahead log
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

## Bước tiếp theo

Append-only log: ghi mỗi thay đổi xuống đĩa trước khi áp vào memory, khởi động lại thì
replay để dựng lại state. Định dạng record cho việc đó đã xong ở phần 03.

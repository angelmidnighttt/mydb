# mydb

Một key-value store viết từ đầu bằng Go, làm để học cách một database hoạt động bên dưới.

**Trạng thái:** dữ liệu đã sống sót qua các lần khởi động lại nhờ write-ahead log. Chưa có
network layer, và chưa chịu được log ghi dở do mất điện.

## Cấu trúc

```
mydb/
├── main.go                    # demo tạm thời: mở db, ghi một key rồi thoát
├── Makefile                   # build / test / run
├── docs/                      # tài liệu, đọc theo thứ tự số
└── internal/
    ├── store/                 # KV trong RAM: map + RWMutex
    ├── wal/                   # định dạng record + file log append-only
    └── kv/                    # ghép log với store: ghi log trước, replay khi mở
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

## Bước tiếp theo

Chống hỏng log: thêm checksum cho từng record, và khi khởi động gặp record ghi dở ở cuối
file thì cắt bỏ rồi chạy tiếp, thay vì từ chối mở database như hiện nay.

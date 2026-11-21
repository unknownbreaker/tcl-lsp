//! bench: scanner and parser throughput over the corpus ($TCL_CORPUS, else the
//! Homebrew Tcl library), for research/09. Each stage runs one warm-up pass and
//! then $BENCH_PASSES timed passes (default 20, at least 1) over the whole
//! corpus ("all") and over its largest file ("largest"), and prints the median
//! MB/s (1 MB = 10^6 bytes, as Go reports it) with the allocations of one pass.
//!
//!   cargo run --release --bin bench

use std::alloc::{GlobalAlloc, Layout, System};
use std::hint::black_box;
use std::sync::atomic::{AtomicU64, Ordering::Relaxed};
use std::time::Instant;

struct Counting;
static ALLOCS: AtomicU64 = AtomicU64::new(0);
static BYTES: AtomicU64 = AtomicU64::new(0);

unsafe impl GlobalAlloc for Counting {
    unsafe fn alloc(&self, l: Layout) -> *mut u8 {
        ALLOCS.fetch_add(1, Relaxed);
        BYTES.fetch_add(l.size() as u64, Relaxed);
        unsafe { System.alloc(l) }
    }
    unsafe fn dealloc(&self, p: *mut u8, l: Layout) {
        unsafe { System.dealloc(p, l) }
    }
    unsafe fn realloc(&self, p: *mut u8, l: Layout, n: usize) -> *mut u8 {
        ALLOCS.fetch_add(1, Relaxed);
        BYTES.fetch_add(n as u64, Relaxed);
        unsafe { System.realloc(p, l, n) }
    }
}

#[global_allocator]
static GLOBAL: Counting = Counting;

fn main() {
    let root = tclparse::corpus_root();
    let passes: usize = std::env::var("BENCH_PASSES")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(20)
        .max(1);
    tclparse::with_big_stack(move || {
        let files = match tclparse::load_corpus(&root) {
            Ok(f) if !f.is_empty() => f,
            Ok(_) => {
                println!("bench: no .tcl files under {}; skipping", root.display());
                return;
            }
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
                println!("bench: no corpus at {}; skipping", root.display());
                return;
            }
            Err(e) => {
                eprintln!("bench: {}: {e}", root.display());
                std::process::exit(1);
            }
        };
        let total: usize = files.iter().map(|f| f.1.len()).sum();
        // The first of equal-sized files, as the Go benchmark picks.
        let largest = files.iter().rev().max_by_key(|f| f.1.len()).unwrap();
        println!(
            "corpus {}: {} files, {} bytes; largest {} ({} bytes); {passes} passes",
            root.display(),
            files.len(),
            total,
            largest.0,
            largest.1.len()
        );
        let all: Vec<&[u8]> = files.iter().map(|f| f.1.as_slice()).collect();
        let one: Vec<&[u8]> = vec![largest.1.as_slice()];
        for (stage, run) in [
            (
                "scan",
                (|s: &[u8]| drop(black_box(tclparse::scan(black_box(s))))) as fn(&[u8]),
            ),
            ("words", |s: &[u8]| {
                drop(black_box(tclparse::parse(black_box(s))))
            }),
        ] {
            for (set, srcs) in [("all", &all), ("largest", &one)] {
                let bytes: usize = srcs.iter().map(|s| s.len()).sum();
                let pass = || srcs.iter().for_each(|s| run(s));
                pass();
                let (a0, b0) = (ALLOCS.load(Relaxed), BYTES.load(Relaxed));
                pass();
                let (allocs, abytes) = (ALLOCS.load(Relaxed) - a0, BYTES.load(Relaxed) - b0);
                let mut secs: Vec<f64> = (0..passes)
                    .map(|_| {
                        let t = Instant::now();
                        pass();
                        t.elapsed().as_secs_f64()
                    })
                    .collect();
                secs.sort_by(f64::total_cmp);
                let mbs = |s: f64| bytes as f64 / s / 1e6;
                println!(
                    "{stage}/{set}: median {:.2} ms, {:.1} MB/s (min {:.1}, max {:.1}); {allocs} allocs, {abytes} B per pass",
                    secs[passes / 2] * 1e3, mbs(secs[passes / 2]), mbs(secs[passes - 1]), mbs(secs[0])
                );
            }
        }
    });
}

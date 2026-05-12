function bar() {
    return 1;
}

export function baz(x: number): number {
    return x + 1;
}

export const qux = (s: string) => s.trim();

class Widget {
    render() {
        return "<div/>";
    }
}

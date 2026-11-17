package io.github.fkzhao.lightwightns;

public final class AgentRpcException extends RuntimeException {
    private final int code;

    public AgentRpcException(int code, String message) {
        super(message);
        this.code = code;
    }

    public int code() { return code; }
}

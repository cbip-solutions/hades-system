//! Serialization trait + impls (excerpt from serde::ser).
use std::fmt;

/// A data structure that can be serialized into any data format supported by Serde.
///
/// Serde provides `Serialize` implementations for many Rust primitive and
/// standard library types. The complete list is available in the [data model
/// section of the manual](https://serde.rs/data-model.html).
pub trait Serialize {
    /// Serialize this value into the given Serde serializer.
    ///
    /// See the [Implementing `Serialize`](https://serde.rs/impl-serialize.html)
    /// section of the manual for more information about how to implement this
    /// method.
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer;
}

impl<T: Serialize + ?Sized> Serialize for Box<T> {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        (**self).serialize(serializer)
    }
}

pub struct Compound<S> {
    ser: S,
    state: State,
}

#[derive(Debug, Copy, Clone, PartialEq, Eq)]
pub enum State {
    Empty,
    First,
    Rest,
}

impl<S> Compound<S>
where
    S: Serializer,
{
    pub fn new(ser: S) -> Self {
        Compound { ser, state: State::Empty }
    }
}

pub fn helper(value: u32) -> String {
    format!("value = {}", value)
}
